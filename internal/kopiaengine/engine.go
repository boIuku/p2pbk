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

type RepositoryStatus struct {
	ConfigPath    string
	StorageType   string
	SnapshotCount int
	UniqueID      []byte
}

type SnapshotInfo struct {
	ID          manifest.ID
	Source      snapshot.SourceInfo
	Description string
	StartTime   string
	EndTime     string
	Root        string
	Stats       snapshot.Stats
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
	return SnapshotInfo{
		ID:          man.ID,
		Source:      man.Source,
		Description: man.Description,
		StartTime:   man.StartTime.Format(time.RFC3339),
		EndTime:     man.EndTime.Format(time.RFC3339),
		Root:        man.RootObjectID().String(),
		Stats:       man.Stats,
	}
}

func (e *Engine) Backup(ctx context.Context, sourcePath, description string) (BackupResult, error) {
	return secrets.WithPassword(e.Password, func() (BackupResult, error) {
		return e.backup(ctx, sourcePath, description)
	})
}

func (e *Engine) backup(ctx context.Context, sourcePath, description string) (BackupResult, error) {
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
	policyTree, err := policy.TreeForSource(ctx, rep, sourceInfo)
	if err != nil {
		return BackupResult{}, fmt.Errorf("load kopia policy: %w", err)
	}

	wctx, writer, err := rep.NewWriter(ctx, repo.WriteSessionOptions{Purpose: "p2pbk backup"})
	if err != nil {
		return BackupResult{}, fmt.Errorf("open kopia writer: %w", err)
	}
	uploader := snapshotfs.NewUploader(writer)
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

func (e *Engine) Restore(ctx context.Context, snapshotRef, targetPath string) (RestoreResult, error) {
	return secrets.WithPassword(e.Password, func() (RestoreResult, error) {
		return e.restore(ctx, snapshotRef, targetPath)
	})
}

func (e *Engine) restore(ctx context.Context, snapshotRef, targetPath string) (RestoreResult, error) {
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
		OverwriteDirectories:   true,
		OverwriteFiles:         true,
		OverwriteSymlinks:      true,
		WriteFilesAtomically:   true,
		IgnorePermissionErrors: true,
	}
	if err := output.Init(ctx); err != nil {
		return RestoreResult{}, fmt.Errorf("initialize restore output: %w", err)
	}
	stats, err := restore.Entry(ctx, rep, output, rootEntry, restore.Options{
		RestoreDirEntryAtDepth: math.MaxInt32,
	})
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore kopia snapshot: %w", err)
	}
	return RestoreResult{ManifestID: man.ID, Stats: stats}, nil
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
	out := make([]SnapshotInfo, 0, len(ids))
	for _, id := range ids {
		man, err := snapshot.LoadSnapshot(ctx, rep, id)
		if err != nil {
			return nil, err
		}
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
	var latest *snapshot.Manifest
	for _, id := range ids {
		man, err := snapshot.LoadSnapshot(ctx, rep, id)
		if err != nil {
			return nil, err
		}
		if latest == nil || man.StartTime.After(latest.StartTime) {
			latest = man
		}
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
