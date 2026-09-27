package kopiaengine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kopia/kopia/fs/localfs"
	"github.com/kopia/kopia/repo"
	kopiablob "github.com/kopia/kopia/repo/blob"
	"github.com/kopia/kopia/repo/maintenance"
	"github.com/kopia/kopia/repo/manifest"
	"github.com/kopia/kopia/snapshot"
	"github.com/kopia/kopia/snapshot/policy"
	"github.com/kopia/kopia/snapshot/restore"
	"github.com/kopia/kopia/snapshot/snapshotfs"
	"github.com/kopia/kopia/snapshot/snapshotmaintenance"

	"p2pbk/internal/config"
	"p2pbk/internal/node"
	"p2pbk/internal/secrets"
)

const (
	storageType        = "p2pbk"
	repositoryFileName = "kopia.repository.config"
)

var ErrRepositoryNotConnected = errors.New("kopia repository is not initialized or connected")

type StorageOptions struct {
	ConfigPath string `json:"configPath"`
}

type Engine struct {
	ConfigPath string
	Config     config.Config
	Password   string
}

type BackupResult struct {
	ManifestID manifest.ID
	Source     snapshot.SourceInfo
	Root       string
}

type RestoreResult struct {
	ManifestID manifest.ID
	Stats      restore.Stats
}

type RestoreOptions struct {
	AllowOverwrite bool
}

type Progress struct {
	BytesDone  int64
	BytesTotal int64
	Message    string
}

type RepositoryStatus struct {
	ConfigPath    string
	StorageType   string
	SnapshotCount int
	UniqueID      []byte
}

type SnapshotInfo struct {
	ID           manifest.ID
	Source       snapshot.SourceInfo
	Description  string
	StartTime    string
	EndTime      string
	Root         string
	Stats        snapshot.Stats
	StorageBytes int64
}

type MaintenanceStatus struct {
	NextFull  time.Time
	NextQuick time.Time
}

func New(configPath string, cfg config.Config, password string) *Engine {
	return &Engine{ConfigPath: configPath, Config: cfg, Password: password}
}

func (e *Engine) EnsureRepository(ctx context.Context) error {
	_, err := secrets.WithPassword(e.Password, func() (struct{}, error) {
		return struct{}{}, e.ensureRepository(ctx)
	})
	return err
}

func (e *Engine) ensureRepository(ctx context.Context) error {
	st, err := e.openStorage(ctx, true)
	if err != nil {
		return err
	}
	defer st.Close(ctx)

	if err := repo.Initialize(ctx, st, &repo.NewRepositoryOptions{}, e.Password); err != nil && !errors.Is(err, repo.ErrAlreadyInitialized) {
		return fmt.Errorf("initialize kopia repository: %w", err)
	}
	if err := repo.Connect(ctx, e.RepositoryConfigPath(), st, e.Password, &repo.ConnectOptions{}); err != nil {
		return fmt.Errorf("connect kopia repository: %w", err)
	}
	return nil
}

func snapshotInfo(man *snapshot.Manifest) SnapshotInfo {
	var storageBytes int64
	if man.StorageStats != nil {
		storageBytes = man.StorageStats.NewData.PackedContentBytes
	}
	return SnapshotInfo{
		ID:           man.ID,
		Source:       man.Source,
		Description:  man.Description,
		StartTime:    man.StartTime.Format(time.RFC3339),
		EndTime:      man.EndTime.Format(time.RFC3339),
		Root:         man.RootObjectID().String(),
		Stats:        man.Stats,
		StorageBytes: storageBytes,
	}
}

func (e *Engine) Backup(ctx context.Context, sourcePath, description string) (BackupResult, error) {
	return e.BackupWithProgress(ctx, sourcePath, description, nil)
}

func (e *Engine) BackupWithProgress(ctx context.Context, sourcePath, description string, progress func(Progress)) (BackupResult, error) {
	return secrets.WithPassword(e.Password, func() (BackupResult, error) {
		return e.backup(ctx, sourcePath, description, progress)
	})
}

func (e *Engine) backup(ctx context.Context, sourcePath, description string, progress func(Progress)) (BackupResult, error) {
	if err := e.validateSourceLayout(sourcePath); err != nil {
		return BackupResult{}, err
	}
	if err := e.ensureRepository(ctx); err != nil {
		return BackupResult{}, err
	}
	rep, err := e.openRepository(ctx)
	if err != nil {
		return BackupResult{}, err
	}
	defer rep.Close(ctx)

	sourceEntry, err := localfs.NewEntry(sourcePath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("open backup source: %w", err)
	}
	sourceInfo, err := e.sourceInfo(sourcePath)
	if err != nil {
		return BackupResult{}, err
	}
	var policyTree *policy.Tree
	if !e.Config.Backup.Incremental {
		policyTree, err = policy.TreeForSourceWithOverride(ctx, rep, sourceInfo, &policy.Policy{NoParent: true})
	} else if e.Config.Backup.InitialFull {
		existing, listErr := snapshot.ListSnapshots(ctx, rep, sourceInfo)
		if listErr != nil {
			return BackupResult{}, fmt.Errorf("list source snapshots: %w", listErr)
		}
		if len(existing) == 0 {
			policyTree, err = policy.TreeForSourceWithOverride(ctx, rep, sourceInfo, &policy.Policy{NoParent: true})
		} else {
			policyTree, err = policy.TreeForSource(ctx, rep, sourceInfo)
		}
	} else {
		policyTree, err = policy.TreeForSource(ctx, rep, sourceInfo)
	}
	if err != nil {
		return BackupResult{}, fmt.Errorf("load kopia policy: %w", err)
	}
	e.excludeProductState(sourcePath, policyTree)

	wctx, writer, err := rep.NewWriter(ctx, repo.WriteSessionOptions{Purpose: "p2pbk backup"})
	if err != nil {
		return BackupResult{}, fmt.Errorf("open kopia writer: %w", err)
	}
	defer writer.Close(wctx)
	uploader := snapshotfs.NewUploader(writer)
	if progress != nil {
		uploader.Progress = &backupProgress{progress: progress}
	}
	man, err := uploader.Upload(wctx, sourceEntry, policyTree, sourceInfo)
	if err != nil {
		return BackupResult{}, fmt.Errorf("upload kopia snapshot: %w", err)
	}
	man.Description = description
	id, err := snapshot.SaveSnapshot(wctx, writer, man)
	if err != nil {
		return BackupResult{}, fmt.Errorf("save kopia snapshot manifest: %w", err)
	}
	if err := writer.Flush(wctx); err != nil {
		return BackupResult{}, fmt.Errorf("flush kopia repository: %w", err)
	}
	return BackupResult{ManifestID: id, Source: sourceInfo, Root: sourcePath}, nil
}

func (e *Engine) validateSourceLayout(sourcePath string) error {
	if sourcePath == "" {
		return errors.New("backup source is required")
	}
	paths, err := e.Config.Paths()
	if err != nil {
		return err
	}
	source, err := canonicalExistingPath(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve backup source: %w", err)
	}
	for _, state := range e.productStatePaths(paths) {
		resolved, resolveErr := canonicalExistingPath(state)
		if resolveErr != nil {
			if !errors.Is(resolveErr, os.ErrNotExist) {
				continue
			}
			resolved = filepath.Clean(state)
		}
		if sameOrChildPath(source, resolved) {
			return fmt.Errorf("backup source %q is inside p2pbk product state %q; choose the source parent or a separate data directory", sourcePath, state)
		}
	}
	return nil
}

func (e *Engine) excludeProductState(sourcePath string, tree *policy.Tree) {
	if tree == nil || tree.EffectivePolicy() == nil {
		return
	}
	paths, err := e.Config.Paths()
	if err != nil {
		return
	}
	source, err := canonicalExistingPath(sourcePath)
	if err != nil {
		return
	}
	for _, state := range e.productStatePaths(paths) {
		resolved, resolveErr := canonicalExistingPath(state)
		if resolveErr != nil {
			resolved = filepath.Clean(state)
		}
		rel, err := filepath.Rel(source, resolved)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		rule := filepath.ToSlash(rel)
		tree.EffectivePolicy().FilesPolicy.IgnoreRules = append(tree.EffectivePolicy().FilesPolicy.IgnoreRules, rule, rule+"/**")
	}
}

func (e *Engine) productStatePaths(paths *config.Paths) []string {
	return []string{
		paths.DataDir,
		paths.StoreDir,
		paths.CacheDir,
		paths.LocalDB,
		paths.Identity,
		e.RepositoryConfigPath(),
	}
}

func canonicalExistingPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if _, statErr := os.Stat(abs); statErr == nil {
			// Some Windows filesystems deny symlink inspection even when the
			// path is readable. Preserve the usable absolute path; traversal
			// checks still reject explicit product-state overlap.
			return filepath.Clean(abs), nil
		}
		return filepath.Clean(abs), err
	}
	return filepath.Clean(resolved), nil
}

func sameOrChildPath(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func (e *Engine) Restore(ctx context.Context, snapshotRef, targetPath string) (RestoreResult, error) {
	return e.RestoreWithOptions(ctx, snapshotRef, targetPath, RestoreOptions{}, nil)
}

func (e *Engine) RestoreWithProgress(ctx context.Context, snapshotRef, targetPath string, progress func(Progress)) (RestoreResult, error) {
	return e.RestoreWithOptions(ctx, snapshotRef, targetPath, RestoreOptions{}, progress)
}

func (e *Engine) RestoreWithOptions(ctx context.Context, snapshotRef, targetPath string, options RestoreOptions, progress func(Progress)) (RestoreResult, error) {
	return secrets.WithPassword(e.Password, func() (RestoreResult, error) {
		return e.restore(ctx, snapshotRef, targetPath, options, progress)
	})
}

func (e *Engine) restore(ctx context.Context, snapshotRef, targetPath string, options RestoreOptions, progress func(Progress)) (RestoreResult, error) {
	if err := validateRestoreTarget(targetPath, options.AllowOverwrite); err != nil {
		return RestoreResult{}, err
	}
	rep, err := e.openRepository(ctx)
	if err != nil {
		return RestoreResult{}, err
	}
	defer rep.Close(ctx)

	man, err := e.resolveSnapshot(ctx, rep, snapshotRef)
	if err != nil {
		return RestoreResult{}, err
	}
	rootEntry := snapshotfs.EntryFromDirEntry(rep, man.RootEntry)
	output := &restore.FilesystemOutput{
		TargetPath:             targetPath,
		OverwriteDirectories:   options.AllowOverwrite,
		OverwriteFiles:         options.AllowOverwrite,
		OverwriteSymlinks:      options.AllowOverwrite,
		WriteFilesAtomically:   true,
		IgnorePermissionErrors: true,
	}
	if err := output.Init(ctx); err != nil {
		return RestoreResult{}, fmt.Errorf("initialize restore output: %w", err)
	}
	restoreOptions := restore.Options{
		RestoreDirEntryAtDepth: math.MaxInt32,
	}
	if progress != nil {
		total := man.Stats.TotalFileSize
		restoreOptions.ProgressCallback = func(ctx context.Context, stats restore.Stats) {
			progress(Progress{
				BytesDone:  stats.RestoredTotalFileSize,
				BytesTotal: total,
				Message:    "restoring files",
			})
		}
	}
	stats, err := restore.Entry(ctx, rep, output, rootEntry, restoreOptions)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore kopia snapshot: %w", err)
	}
	return RestoreResult{ManifestID: man.ID, Stats: stats}, nil
}

func validateRestoreTarget(targetPath string, allowOverwrite bool) error {
	if targetPath == "" {
		return errors.New("restore target is required")
	}
	info, err := os.Stat(targetPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect restore target: %w", err)
	}
	if allowOverwrite {
		return nil
	}
	if !info.IsDir() {
		return fmt.Errorf("restore target %q already exists; pass --overwrite to replace conflicting paths", targetPath)
	}
	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return fmt.Errorf("inspect restore target: %w", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("restore target %q is not empty; pass --overwrite to replace conflicting paths", targetPath)
	}
	return nil
}

type backupProgress struct {
	snapshotfs.CountingUploadProgress
	progress func(Progress)
}

func (p *backupProgress) UploadStarted() {
	p.CountingUploadProgress.UploadStarted()
	p.report("scanning files")
}

func (p *backupProgress) EstimatedDataSize(fileCount int, totalBytes int64) {
	p.CountingUploadProgress.EstimatedDataSize(fileCount, totalBytes)
	p.report("estimated backup size")
}

func (p *backupProgress) CachedFile(path string, size int64) {
	p.CountingUploadProgress.CachedFile(path, size)
	p.report("reusing unchanged files")
}

func (p *backupProgress) HashedBytes(numBytes int64) {
	p.CountingUploadProgress.HashedBytes(numBytes)
	p.report("backing up files")
}

func (p *backupProgress) FinishedHashingFile(fname string, numBytes int64) {
	p.CountingUploadProgress.FinishedHashingFile(fname, numBytes)
	p.report("backing up files")
}

func (p *backupProgress) StartedDirectory(dirname string) {
	p.CountingUploadProgress.StartedDirectory(dirname)
	p.report("scanning " + dirname)
}

func (p *backupProgress) UploadFinished() {
	p.CountingUploadProgress.UploadFinished()
	p.report("backup data written")
}

func (p *backupProgress) report(message string) {
	if p.progress == nil {
		return
	}
	counters := p.Snapshot()
	done := counters.TotalHashedBytes + counters.TotalCachedBytes
	p.progress(Progress{
		BytesDone:  done,
		BytesTotal: counters.EstimatedBytes,
		Message:    message,
	})
}

func (e *Engine) Status(ctx context.Context) (RepositoryStatus, error) {
	return secrets.WithPassword(e.Password, func() (RepositoryStatus, error) {
		return e.status(ctx)
	})
}

func (e *Engine) status(ctx context.Context) (RepositoryStatus, error) {
	rep, err := e.openRepository(ctx)
	if err != nil {
		return RepositoryStatus{}, err
	}
	defer rep.Close(ctx)

	snapshots, err := e.listSnapshots(ctx)
	if err != nil {
		return RepositoryStatus{}, err
	}
	status := RepositoryStatus{
		ConfigPath:    e.RepositoryConfigPath(),
		StorageType:   storageType,
		SnapshotCount: len(snapshots),
	}
	if direct, ok := rep.(repo.DirectRepository); ok {
		status.UniqueID = append([]byte(nil), direct.UniqueID()...)
	}
	return status, nil
}

func (e *Engine) ListSnapshots(ctx context.Context) ([]SnapshotInfo, error) {
	return secrets.WithPassword(e.Password, func() ([]SnapshotInfo, error) {
		return e.listSnapshots(ctx)
	})
}

func (e *Engine) listSnapshots(ctx context.Context) ([]SnapshotInfo, error) {
	rep, err := e.openRepository(ctx)
	if err != nil {
		return nil, err
	}
	defer rep.Close(ctx)

	ids, err := snapshot.ListSnapshotManifests(ctx, rep, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list kopia snapshots: %w", err)
	}
	manifests := make([]*snapshot.Manifest, 0, len(ids))
	for _, id := range ids {
		man, err := snapshot.LoadSnapshot(ctx, rep, id)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, man)
	}
	if len(manifests) > 0 {
		chronological := snapshot.SortByTime(manifests, false)
		if err := snapshotfs.CalculateStorageStats(ctx, rep, chronological, func(m *snapshot.Manifest) error {
			return nil
		}); err != nil {
			return nil, fmt.Errorf("calculate snapshot storage stats: %w", err)
		}
	}
	out := make([]SnapshotInfo, 0, len(manifests))
	for _, man := range manifests {
		out = append(out, snapshotInfo(man))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartTime > out[j].StartTime
	})
	return out, nil
}

func (e *Engine) ShowSnapshot(ctx context.Context, ref string) (SnapshotInfo, error) {
	return secrets.WithPassword(e.Password, func() (SnapshotInfo, error) {
		return e.showSnapshot(ctx, ref)
	})
}

func (e *Engine) showSnapshot(ctx context.Context, ref string) (SnapshotInfo, error) {
	rep, err := e.openRepository(ctx)
	if err != nil {
		return SnapshotInfo{}, err
	}
	defer rep.Close(ctx)

	man, err := e.resolveSnapshot(ctx, rep, ref)
	if err != nil {
		return SnapshotInfo{}, err
	}
	return snapshotInfo(man), nil
}

func (e *Engine) MaintenanceStatus(ctx context.Context) (MaintenanceStatus, error) {
	return secrets.WithPassword(e.Password, func() (MaintenanceStatus, error) {
		return e.maintenanceStatus(ctx)
	})
}

// ApplyRetention persists the configured retention policy for a source and
// applies it through Kopia's supported manifest deletion primitive. The
// latest complete snapshot is always retained in addition to the configured
// time window.
func (e *Engine) ApplyRetention(ctx context.Context, sourcePath, retention string) (int, error) {
	return secrets.WithPassword(e.Password, func() (int, error) {
		return e.applyRetention(ctx, sourcePath, retention)
	})
}

func (e *Engine) applyRetention(ctx context.Context, sourcePath, retention string) (int, error) {
	retentionPolicy, err := parseRetentionPolicy(retention)
	if err != nil {
		return 0, err
	}
	rep, err := e.openRepository(ctx)
	if err != nil {
		return 0, err
	}
	defer rep.Close(ctx)
	sourceInfo, err := e.sourceInfo(sourcePath)
	if err != nil {
		return 0, err
	}
	defined, err := policy.GetDefinedPolicy(ctx, rep, sourceInfo)
	if err != nil && !errors.Is(err, policy.ErrPolicyNotFound) {
		return 0, fmt.Errorf("load retention policy: %w", err)
	}
	updated := &policy.Policy{}
	if defined != nil {
		*updated = *defined
	}
	updated.RetentionPolicy = retentionPolicy
	wctx, writer, err := rep.NewWriter(ctx, repo.WriteSessionOptions{Purpose: "p2pbk retention policy"})
	if err != nil {
		return 0, fmt.Errorf("open retention writer: %w", err)
	}
	if err := policy.SetPolicy(wctx, writer, sourceInfo, updated); err != nil {
		_ = writer.Close(wctx)
		return 0, err
	}
	if err := writer.Flush(wctx); err != nil {
		_ = writer.Close(wctx)
		return 0, fmt.Errorf("flush retention policy: %w", err)
	}
	if err := writer.Close(wctx); err != nil {
		return 0, fmt.Errorf("close retention policy writer: %w", err)
	}

	wctx, writer, err = rep.NewWriter(ctx, repo.WriteSessionOptions{Purpose: "p2pbk retention"})
	if err != nil {
		return 0, fmt.Errorf("open retention writer: %w", err)
	}
	deleted, err := policy.ApplyRetentionPolicy(wctx, writer, sourceInfo, true)
	if err != nil {
		_ = writer.Close(wctx)
		return 0, fmt.Errorf("apply retention policy: %w", err)
	}
	if err := writer.Flush(wctx); err != nil {
		_ = writer.Close(wctx)
		return 0, fmt.Errorf("flush retention deletion: %w", err)
	}
	if err := writer.Close(wctx); err != nil {
		return 0, fmt.Errorf("close retention writer: %w", err)
	}
	return len(deleted), nil
}

// RunMaintenance invokes Kopia's complete snapshot/repository maintenance
// path. Callers must serialize this with backup/restore operations.
func (e *Engine) RunMaintenance(ctx context.Context) error {
	_, err := secrets.WithPassword(e.Password, func() (struct{}, error) {
		rep, err := e.openRepository(ctx)
		if err != nil {
			return struct{}{}, err
		}
		defer rep.Close(ctx)
		direct, ok := rep.(repo.DirectRepository)
		if !ok {
			return struct{}{}, errors.New("repository does not expose direct maintenance state")
		}
		wctx, writer, err := direct.NewDirectWriter(ctx, repo.WriteSessionOptions{Purpose: "p2pbk maintenance"})
		if err != nil {
			return struct{}{}, fmt.Errorf("open maintenance writer: %w", err)
		}
		if err := snapshotmaintenance.Run(wctx, writer, maintenance.ModeFull, true, maintenance.SafetyFull); err != nil {
			_ = writer.Close(wctx)
			return struct{}{}, fmt.Errorf("run repository maintenance: %w", err)
		}
		if err := writer.Close(wctx); err != nil {
			return struct{}{}, fmt.Errorf("close maintenance writer: %w", err)
		}
		return struct{}{}, nil
	})
	return err
}

// PinSnapshots updates the stable recovery pin across all snapshot manifests.
// It is idempotent and removes the same pin from snapshots outside ids.
func (e *Engine) PinSnapshots(ctx context.Context, ids []string, pin string) error {
	_, err := secrets.WithPassword(e.Password, func() (struct{}, error) {
		if pin == "" {
			return struct{}{}, errors.New("snapshot pin is required")
		}
		rep, err := e.openRepository(ctx)
		if err != nil {
			return struct{}{}, err
		}
		defer rep.Close(ctx)
		wctx, writer, err := rep.NewWriter(ctx, repo.WriteSessionOptions{Purpose: "p2pbk recovery pin"})
		if err != nil {
			return struct{}{}, fmt.Errorf("open recovery pin writer: %w", err)
		}
		targets := map[manifest.ID]struct{}{}
		for _, id := range ids {
			if id != "" {
				targets[manifest.ID(id)] = struct{}{}
			}
		}
		manifestIDs, err := snapshot.ListSnapshotManifests(wctx, writer, nil, nil)
		if err != nil {
			_ = writer.Close(wctx)
			return struct{}{}, fmt.Errorf("list snapshots for recovery pin: %w", err)
		}
		for _, id := range manifestIDs {
			m, err := snapshot.LoadSnapshot(wctx, writer, id)
			if err != nil {
				_ = writer.Close(wctx)
				return struct{}{}, fmt.Errorf("load snapshot %s for recovery pin: %w", id, err)
			}
			if _, keep := targets[id]; keep {
				m.UpdatePins([]string{pin}, nil)
			} else {
				m.UpdatePins(nil, []string{pin})
			}
			if err := snapshot.UpdateSnapshot(wctx, writer, m); err != nil {
				_ = writer.Close(wctx)
				return struct{}{}, fmt.Errorf("update recovery pin for snapshot %s: %w", id, err)
			}
		}
		if err := writer.Flush(wctx); err != nil {
			_ = writer.Close(wctx)
			return struct{}{}, fmt.Errorf("flush recovery pins: %w", err)
		}
		if err := writer.Close(wctx); err != nil {
			return struct{}{}, fmt.Errorf("close recovery pin writer: %w", err)
		}
		return struct{}{}, nil
	})
	return err
}

func parseRetentionPolicy(value string) (policy.RetentionPolicy, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return policy.RetentionPolicy{}, errors.New("backup.retention is required")
	}
	latest := policy.OptionalInt(1)
	result := policy.RetentionPolicy{KeepLatest: &latest}
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil || days <= 0 {
			return policy.RetentionPolicy{}, fmt.Errorf("backup.retention must be a positive duration such as 30d or 48h")
		}
		keep := policy.OptionalInt(days)
		result.KeepDaily = &keep
		return result, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return policy.RetentionPolicy{}, fmt.Errorf("backup.retention must be a positive duration such as 30d or 48h")
	}
	hours := int((duration + time.Hour - 1) / time.Hour)
	keep := policy.OptionalInt(hours)
	result.KeepHourly = &keep
	return result, nil
}

func (e *Engine) maintenanceStatus(ctx context.Context) (MaintenanceStatus, error) {
	rep, err := e.openRepository(ctx)
	if err != nil {
		return MaintenanceStatus{}, err
	}
	defer rep.Close(ctx)

	direct, ok := rep.(repo.DirectRepository)
	if !ok {
		return MaintenanceStatus{}, errors.New("repository does not expose direct maintenance state")
	}
	s, err := maintenance.GetSchedule(ctx, direct)
	if err != nil {
		return MaintenanceStatus{}, fmt.Errorf("get maintenance schedule: %w", err)
	}
	return MaintenanceStatus{NextFull: s.NextFullMaintenanceTime, NextQuick: s.NextQuickMaintenanceTime}, nil
}

func (e *Engine) openRepository(ctx context.Context) (repo.Repository, error) {
	RegisterProvider()
	if _, err := os.Stat(e.RepositoryConfigPath()); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: run `p2pbk repo init` or `p2pbk backup now` first", ErrRepositoryNotConnected)
	} else if err != nil {
		return nil, fmt.Errorf("stat kopia repository config: %w", err)
	}
	rep, err := secrets.WithPassword(e.Password, func() (repo.Repository, error) {
		return repo.Open(ctx, e.RepositoryConfigPath(), e.Password, &repo.Options{})
	})
	if err != nil {
		return nil, fmt.Errorf("open kopia repository: %w", err)
	}
	return rep, nil
}

func (e *Engine) RepositoryConnected() bool {
	_, err := os.Stat(e.RepositoryConfigPath())
	return err == nil
}

func (e *Engine) openStorage(ctx context.Context, isCreate bool) (*openedStorage, error) {
	RegisterProvider()
	st, err := secrets.WithPassword(e.Password, func() (kopiablob.Storage, error) {
		return newStorage(ctx, &StorageOptions{ConfigPath: e.ConfigPath}, isCreate)
	})
	if err != nil {
		return nil, err
	}
	opened, ok := st.(*openedStorage)
	if !ok {
		return nil, fmt.Errorf("unexpected p2pbk storage type %T", st)
	}
	return opened, nil
}

func (e *Engine) RepositoryConfigPath() string {
	paths, err := e.Config.Paths()
	if err != nil {
		return repositoryFileName
	}
	return filepath.Join(paths.DataDir, repositoryFileName)
}

func (e *Engine) sourceInfo(path string) (snapshot.SourceInfo, error) {
	host, _ := os.Hostname()
	username := ""
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	return snapshot.ParseSourceInfo(path, host, username)
}

func (e *Engine) resolveSnapshot(ctx context.Context, rep repo.Repository, ref string) (*snapshot.Manifest, error) {
	if ref == "" || ref == "latest" {
		return latestSnapshot(ctx, rep)
	}
	if man, err := snapshot.LoadSnapshot(ctx, rep, manifest.ID(ref)); err == nil {
		return man, nil
	}
	return nil, fmt.Errorf("snapshot %q not found; use a Kopia manifest ID or \"latest\"", ref)
}

func latestSnapshot(ctx context.Context, rep repo.Repository) (*snapshot.Manifest, error) {
	ids, err := snapshot.ListSnapshotManifests(ctx, rep, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list kopia snapshots: %w", err)
	}
	if len(ids) == 0 {
		return nil, errors.New("no kopia snapshots found")
	}
	paths := map[string]struct{}{}
	var latest *snapshot.Manifest
	for _, id := range ids {
		man, err := snapshot.LoadSnapshot(ctx, rep, id)
		if err != nil {
			return nil, err
		}
		path := filepath.Clean(man.Source.Path)
		paths[path] = struct{}{}
		if latest == nil || man.StartTime.After(latest.StartTime) {
			latest = man
		}
	}
	if len(paths) > 1 {
		return nil, errors.New("latest is ambiguous across multiple source roots; restore by snapshot ID or select an exact root")
	}
	return latest, nil
}

type openedStorage struct {
	kopiablob.Storage
	options StorageOptions
	backend *node.BlobStorage
}

func (s *openedStorage) ConnectionInfo() kopiablob.ConnectionInfo {
	return kopiablob.ConnectionInfo{
		Type:   storageType,
		Config: s.options,
	}
}

func (s *openedStorage) Close(ctx context.Context) error {
	return errors.Join(s.Storage.Close(ctx), s.backend.Close(ctx))
}
