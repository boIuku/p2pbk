package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		return BackupResult{}, errors.New("backup store password is required; enter it in the TUI or set P2PBK_REPOSITORY_PASSWORD")
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
		return RestoreResult{}, errors.New("backup store password is required; enter it in the TUI or set P2PBK_REPOSITORY_PASSWORD")
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
	sources := SplitPathList(req.Source)
	if len(sources) == 1 {
		req.Source = sources[0]
	}
	total := dirBytes(req.Source)
	if req.Config.Backup.DataShards > 0 {
		total = ErasureStorageBytes(total, req.Config.Backup.DataShards, req.Config.Backup.ParityShards)
	}
	paths, pathErr := req.Config.Paths()
	var before int64
	if pathErr == nil {
		before = dirBytes(paths.StoreDir)
	}
	done := make(chan struct{})
	var result BackupResult
	var err error
	go func() {
		defer close(done)
		result, err = BackupRoot(ctx, req)
	}()
	pollOperation(ctx, events, pollRequest{
		ID:         id,
		Kind:       "backup",
		Phase:      "uploading",
		Source:     req.Source,
		BytesTotal: total,
		StartedAt:  started,
		Done:       done,
		Measure: func() int64 {
			if pathErr != nil || paths == nil {
				return 0
			}
			current := dirBytes(paths.StoreDir) - before
			if current < 0 {
				return 0
			}
			return current
		},
		Finish: func() (string, error) {
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("snapshot %s saved for %s", result.ManifestID, result.Source), nil
		},
	})
}

func RestoreSnapshotWithEvents(ctx context.Context, req RestoreRequest, events chan<- OperationEvent) {
	id := operationID("restore")
	started := time.Now()
	before := dirBytes(req.Target)
	done := make(chan struct{})
	var result RestoreResult
	var err error
	go func() {
		defer close(done)
		result, err = RestoreSnapshot(ctx, req)
	}()
	pollOperation(ctx, events, pollRequest{
		ID:        id,
		Kind:      "restore",
		Phase:     "restoring",
		Source:    req.Snapshot,
		Target:    req.Target,
		StartedAt: started,
		Done:      done,
		Measure: func() int64 {
			current := dirBytes(req.Target) - before
			if current < 0 {
				return 0
			}
			return current
		},
		Finish: func() (string, error) {
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("restored %s to %s (%d files, %d dirs)", result.ManifestID, req.Target, result.Files, result.Dirs), nil
		},
	})
}

type pollRequest struct {
	ID         string
	Kind       string
	Phase      string
	Source     string
	Target     string
	BytesTotal int64
	StartedAt  time.Time
	Done       <-chan struct{}
	Measure    func() int64
	Finish     func() (string, error)
}

func pollOperation(ctx context.Context, events chan<- OperationEvent, req pollRequest) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	send := func(done bool, msg string, err error) {
		updated := time.Now()
		bytesDone := req.Measure()
		elapsed := updated.Sub(req.StartedAt).Seconds()
		var speed float64
		var eta time.Duration
		if elapsed > 0 {
			speed = float64(bytesDone) / elapsed
		}
		if speed > 0 && req.BytesTotal > bytesDone {
			eta = time.Duration(float64(req.BytesTotal-bytesDone)/speed) * time.Second
		}
		select {
		case events <- OperationEvent{
			ID:         req.ID,
			Kind:       req.Kind,
			Phase:      req.Phase,
			Source:     req.Source,
			Target:     req.Target,
			BytesDone:  bytesDone,
			BytesTotal: req.BytesTotal,
			SpeedBps:   speed,
			ETA:        eta,
			Message:    msg,
			StartedAt:  req.StartedAt,
			UpdatedAt:  updated,
			Done:       done,
			Err:        err,
		}:
		case <-ctx.Done():
		}
	}
	send(false, "started", nil)
	for {
		select {
		case <-ctx.Done():
			send(true, "cancelled", ctx.Err())
			return
		case <-ticker.C:
			send(false, "running", nil)
		case <-req.Done:
			msg, err := req.Finish()
			send(true, msg, err)
			return
		}
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
