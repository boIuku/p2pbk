package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"p2pbk/internal/config"
	"p2pbk/internal/kopiaengine"
)

type BackupRequest struct {
	Config      config.Config
	ConfigPath  string
	Password    string
	Source      string
	Description string
}

type BackupResult struct {
	ManifestID string
	Source     string
}

type RestoreRequest struct {
	Config     config.Config
	ConfigPath string
	Password   string
	Snapshot   string
	Target     string
}

type RestoreResult struct {
	ManifestID string
	Files      int
	Dirs       int
}

type OperationEvent struct {
	ID         string
	Kind       string
	Phase      string
	Source     string
	Target     string
	BytesDone  int64
	BytesTotal int64
	SpeedBps   float64
	ETA        time.Duration
	Message    string
	StartedAt  time.Time
	UpdatedAt  time.Time
	Done       bool
	Err        error
}

func BackupRoot(ctx context.Context, req BackupRequest) (BackupResult, error) {
	if req.ConfigPath == "" {
		return BackupResult{}, errors.New("p2pbk config file is required; pass --config or run p2pbk init first")
	}
	if req.Password == "" {
		return BackupResult{}, errors.New("p2pbk password is required; enter it in the TUI or set P2PBK_REPOSITORY_PASSWORD")
	}
	if req.Source == "" {
		return BackupResult{}, errors.New("backup source is required")
	}
	sources := SplitPathList(req.Source)
	if len(sources) != 1 {
		return BackupResult{}, fmt.Errorf("backup source must be one directory, got %d; add assets separately", len(sources))
	}
	req.Source = sources[0]
	if info, err := os.Stat(req.Source); err != nil {
		return BackupResult{}, fmt.Errorf("backup source %q: %w", req.Source, err)
	} else if !info.IsDir() {
		return BackupResult{}, fmt.Errorf("backup source %q is not a directory", req.Source)
	}
	if req.Description == "" {
		req.Description = "manual"
	}

	engine := kopiaengine.New(req.ConfigPath, req.Config, req.Password)
	result, err := engine.Backup(ctx, req.Source, req.Description)
	if err != nil {
		return BackupResult{}, err
	}
	return BackupResult{ManifestID: string(result.ManifestID), Source: fmt.Sprint(result.Source)}, nil
}

func BackupAll(ctx context.Context, cfg config.Config, configPath, password, description string) ([]BackupResult, error) {
	if len(cfg.Backup.Roots) == 0 {
		return nil, errors.New("no backup roots configured; run p2pbk init --dir <path> or set backup.roots")
	}
	results := make([]BackupResult, 0, len(cfg.Backup.Roots))
	for _, root := range NormalizeRootList(cfg.Backup.Roots) {
		result, err := BackupRoot(ctx, BackupRequest{
			Config:      cfg,
			ConfigPath:  configPath,
			Password:    password,
			Source:      root,
			Description: description,
		})
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func RestoreSnapshot(ctx context.Context, req RestoreRequest) (RestoreResult, error) {
	if req.ConfigPath == "" {
		return RestoreResult{}, errors.New("p2pbk config file is required; pass --config or run p2pbk init first")
	}
	if req.Password == "" {
		return RestoreResult{}, errors.New("p2pbk password is required; enter it in the TUI or set P2PBK_REPOSITORY_PASSWORD")
	}
	if req.Snapshot == "" {
		return RestoreResult{}, errors.New("snapshot id is required")
	}
	if req.Target == "" {
		return RestoreResult{}, errors.New("restore target is required")
	}

	engine := kopiaengine.New(req.ConfigPath, req.Config, req.Password)
	result, err := engine.Restore(ctx, req.Snapshot, req.Target)
	if err != nil {
		return RestoreResult{}, err
	}
	return RestoreResult{
		ManifestID: string(result.ManifestID),
		Files:      int(result.Stats.RestoredFileCount),
		Dirs:       int(result.Stats.RestoredDirCount),
	}, nil
}

func BackupRootWithEvents(ctx context.Context, req BackupRequest, events chan<- OperationEvent) {
	id := operationID("backup")
	started := time.Now()
	if req.ConfigPath == "" {
		sendImmediateOperationError(ctx, events, id, "backup", "backing up", started, "p2pbk config file is required; pass --config or run p2pbk init first")
		return
	}
	if req.Password == "" {
		sendImmediateOperationError(ctx, events, id, "backup", "backing up", started, "p2pbk password is required; enter it in the TUI or set P2PBK_REPOSITORY_PASSWORD")
		return
	}
	if req.Source == "" {
		sendImmediateOperationError(ctx, events, id, "backup", "backing up", started, "backup source is required")
		return
	}
	sources := SplitPathList(req.Source)
	if len(sources) != 1 {
		sendImmediateOperationError(ctx, events, id, "backup", "backing up", started, fmt.Sprintf("backup source must be one directory, got %d; add assets separately", len(sources)))
		return
	}
	req.Source = sources[0]
	if info, err := os.Stat(req.Source); err != nil {
		sendImmediateOperationError(ctx, events, id, "backup", "backing up", started, fmt.Sprintf("backup source %q: %v", req.Source, err))
		return
	} else if !info.IsDir() {
		sendImmediateOperationError(ctx, events, id, "backup", "backing up", started, fmt.Sprintf("backup source %q is not a directory", req.Source))
		return
	}
	if req.Description == "" {
		req.Description = "manual"
	}
	total := dirBytes(req.Source)
	emitter := newOperationEmitter(ctx, events, operationEventBase{
		ID:         id,
		Kind:       "backup",
		Phase:      "backing up",
		Source:     req.Source,
		BytesTotal: total,
		StartedAt:  started,
	})
	emitter.send(false, 0, total, "starting backup", nil, true)

	engine := kopiaengine.New(req.ConfigPath, req.Config, req.Password)
	var last kopiaengine.Progress
	var lastMu sync.Mutex
	result, err := engine.BackupWithProgress(ctx, req.Source, req.Description, func(progress kopiaengine.Progress) {
		if progress.BytesTotal <= 0 {
			progress.BytesTotal = total
		}
		lastMu.Lock()
		last = progress
		lastMu.Unlock()
		emitter.send(false, progress.BytesDone, progress.BytesTotal, progress.Message, nil, false)
	})
	lastMu.Lock()
	lastProgress := last
	lastMu.Unlock()
	if err != nil {
		emitter.send(true, lastProgress.BytesDone, lastProgress.BytesTotal, "", err, true)
		return
	}
	emitter.send(true, total, total, fmt.Sprintf("snapshot %s saved for %s", result.ManifestID, result.Source), nil, true)
}

func RestoreSnapshotWithEvents(ctx context.Context, req RestoreRequest, events chan<- OperationEvent) {
	id := operationID("restore")
	started := time.Now()
	if req.ConfigPath == "" {
		sendImmediateOperationError(ctx, events, id, "restore", "restoring", started, "p2pbk config file is required; pass --config or run p2pbk init first")
		return
	}
	if req.Password == "" {
		sendImmediateOperationError(ctx, events, id, "restore", "restoring", started, "p2pbk password is required; enter it in the TUI or set P2PBK_REPOSITORY_PASSWORD")
		return
	}
	if req.Snapshot == "" {
		sendImmediateOperationError(ctx, events, id, "restore", "restoring", started, "snapshot id is required")
		return
	}
	if req.Target == "" {
		sendImmediateOperationError(ctx, events, id, "restore", "restoring", started, "restore target is required")
		return
	}
	emitter := newOperationEmitter(ctx, events, operationEventBase{
		ID:        id,
		Kind:      "restore",
		Phase:     "restoring",
		Source:    req.Snapshot,
		Target:    req.Target,
		StartedAt: started,
	})
	emitter.send(false, 0, 0, "starting restore", nil, true)

	engine := kopiaengine.New(req.ConfigPath, req.Config, req.Password)
	var last kopiaengine.Progress
	var lastMu sync.Mutex
	result, err := engine.RestoreWithProgress(ctx, req.Snapshot, req.Target, func(progress kopiaengine.Progress) {
		lastMu.Lock()
		last = progress
		lastMu.Unlock()
		emitter.send(false, progress.BytesDone, progress.BytesTotal, progress.Message, nil, false)
	})
	lastMu.Lock()
	lastProgress := last
	lastMu.Unlock()
	if err != nil {
		emitter.send(true, lastProgress.BytesDone, lastProgress.BytesTotal, "", err, true)
		return
	}
	done := lastProgress.BytesTotal
	if done <= 0 {
		done = lastProgress.BytesDone
	}
	emitter.send(true, done, done, fmt.Sprintf("restored %s to %s (%d files, %d dirs)", result.ManifestID, req.Target, result.Stats.RestoredFileCount, result.Stats.RestoredDirCount), nil, true)
}

func sendImmediateOperationError(ctx context.Context, events chan<- OperationEvent, id, kind, phase string, started time.Time, message string) {
	err := errors.New(message)
	select {
	case events <- OperationEvent{
		ID:        id,
		Kind:      kind,
		Phase:     phase,
		Message:   message,
		StartedAt: started,
		UpdatedAt: time.Now(),
		Done:      true,
		Err:       err,
	}:
	case <-ctx.Done():
	}
}

type operationEventBase struct {
	ID         string
	Kind       string
	Phase      string
	Source     string
	Target     string
	BytesTotal int64
	StartedAt  time.Time
}

type operationEmitter struct {
	ctx      context.Context
	events   chan<- OperationEvent
	base     operationEventBase
	mu       sync.Mutex
	lastSent time.Time
}

func newOperationEmitter(ctx context.Context, events chan<- OperationEvent, base operationEventBase) *operationEmitter {
	return &operationEmitter{ctx: ctx, events: events, base: base}
}

func (e *operationEmitter) send(done bool, bytesDone, bytesTotal int64, msg string, err error, force bool) {
	if bytesTotal <= 0 {
		bytesTotal = e.base.BytesTotal
	}
	if bytesDone < 0 {
		bytesDone = 0
	}
	if bytesTotal > 0 && bytesDone > bytesTotal {
		bytesDone = bytesTotal
	}

	updated := time.Now()
	e.mu.Lock()
	if !force && !e.lastSent.IsZero() && updated.Sub(e.lastSent) < 250*time.Millisecond {
		e.mu.Unlock()
		return
	}
	e.lastSent = updated
	e.mu.Unlock()

	elapsed := updated.Sub(e.base.StartedAt).Seconds()
	var speed float64
	var eta time.Duration
	if elapsed > 0 {
		speed = float64(bytesDone) / elapsed
	}
	if speed > 0 && bytesTotal > bytesDone {
		eta = time.Duration(float64(bytesTotal-bytesDone)/speed) * time.Second
	}

	event := OperationEvent{
		ID:         e.base.ID,
		Kind:       e.base.Kind,
		Phase:      e.base.Phase,
		Source:     e.base.Source,
		Target:     e.base.Target,
		BytesDone:  bytesDone,
		BytesTotal: bytesTotal,
		SpeedBps:   speed,
		ETA:        eta,
		Message:    msg,
		StartedAt:  e.base.StartedAt,
		UpdatedAt:  updated,
		Done:       done,
		Err:        err,
	}
	if done {
		select {
		case e.events <- event:
		case <-e.ctx.Done():
		}
		return
	}
	select {
	case e.events <- event:
	case <-e.ctx.Done():
	default:
	}
}

func operationID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func dirBytes(root string) int64 {
	if strings.Contains(root, ";") {
		return 0
	}
	var total int64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
