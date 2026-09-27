package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"p2pbk/internal/config"
	"p2pbk/internal/peer/localdb"
)

type operationGuard struct {
	mu     sync.Mutex
	lock   *flock.Flock
	db     *localdb.DB
	record localdb.OperationRecord
	done   bool
}

func beginOperation(ctx context.Context, cfg config.Config, configPath, kind, root, snapshot string) (*operationGuard, error) {
	paths, err := cfg.Paths()
	if err != nil {
		return nil, err
	}
	operationsDir := filepath.Join(paths.DataDir, "operations")
	if err := os.MkdirAll(operationsDir, 0o700); err != nil {
		return nil, fmt.Errorf("create operation directory: %w", err)
	}
	lock := flock.New(filepath.Join(operationsDir, kind+".lock"))
	db, err := localdb.Open(paths.LocalDB)
	if err != nil {
		return nil, err
	}
	record := localdb.OperationRecord{
		ID:         operationID(kind),
		Kind:       kind,
		ConfigPath: configPath,
		Root:       root,
		Snapshot:   snapshot,
		State:      "accepted",
		Attempt:    1,
		StartedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := db.PutOperation(record); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("record operation acceptance: %w", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	locked, err := lock.TryLockContext(waitCtx, 100*time.Millisecond)
	if err != nil {
		_ = db.PutOperation(failedOperation(record, err))
		_ = db.Close()
		return nil, err
	}
	if !locked {
		err := errors.New("operation lock not acquired")
		_ = db.PutOperation(failedOperation(record, err))
		_ = db.Close()
		return nil, err
	}
	record.State = "running"
	record.UpdatedAt = time.Now().UTC()
	if err := db.PutOperation(record); err != nil {
		_ = lock.Unlock()
		_ = db.Close()
		return nil, fmt.Errorf("record operation start: %w", err)
	}
	return &operationGuard{lock: lock, db: db, record: record}, nil
}

func (g *operationGuard) finish(err error) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.done {
		return
	}
	g.done = true
	if err == nil {
		g.record.State = "completed"
		g.record.CompletedAt = time.Now().UTC()
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		g.record.State = "retry"
	} else {
		g.record.State = "failed"
	}
	g.record.Error = operationErrorString(err)
	g.record.UpdatedAt = time.Now().UTC()
	_ = g.db.PutOperation(g.record)
	_ = g.lock.Unlock()
	_ = g.db.Close()
}

func failedOperation(record localdb.OperationRecord, err error) localdb.OperationRecord {
	record.State = "failed"
	record.Error = operationErrorString(err)
	record.UpdatedAt = time.Now().UTC()
	return record
}

func operationErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
