package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"p2pbk/internal/config"
	"p2pbk/internal/kopiaengine"
	"p2pbk/internal/recovery"
)

type MaintenanceResult struct {
	DeletedSnapshots int
}

// RunMaintenance applies retention and then runs Kopia maintenance under the
// durable maintenance operation lock. A configured recovery checkpoint must
// authenticate before destructive GC is allowed to proceed.
func RunMaintenance(ctx context.Context, cfg config.Config, configPath, password string) (result MaintenanceResult, err error) {
	if configPath == "" {
		return result, errors.New("p2pbk config file is required; pass --config or run p2pbk init first")
	}
	if password == "" {
		return result, errors.New("p2pbk password is required; enter it in the TUI or set P2PBK_REPOSITORY_PASSWORD")
	}
	guard, err := beginOperation(ctx, cfg, configPath, "maintenance", "", "")
	if err != nil {
		return result, err
	}
	defer func() { guard.finish(err) }()

	engine := kopiaengine.New(configPath, cfg, password)
	if cfg.Recovery.ExternalPath != "" {
		bundlePath := filepath.Join(cfg.Recovery.ExternalPath, "p2pbk-recovery.age")
		metadata, verifyErr := recovery.Verify(ctx, bundlePath, password)
		if verifyErr != nil {
			return result, fmt.Errorf("refuse destructive maintenance until recovery checkpoint verifies: %w", verifyErr)
		}
		if err := engine.PinSnapshots(ctx, metadata.SnapshotIDs, "p2pbk-recovery"); err != nil {
			return result, fmt.Errorf("protect recovery checkpoint snapshots: %w", err)
		}
	}
	for _, root := range NormalizeRootList(cfg.Backup.Roots) {
		deleted, retentionErr := engine.ApplyRetention(ctx, root, cfg.Backup.Retention)
		if retentionErr != nil {
			return result, fmt.Errorf("apply retention for %q: %w", root, retentionErr)
		}
		result.DeletedSnapshots += deleted
	}
	if err := engine.RunMaintenance(ctx); err != nil {
		return result, err
	}
	if cfg.Recovery.ExternalPath != "" {
		_, checkpointErr := publishCheckpoint(ctx, BackupRequest{Config: cfg, ConfigPath: configPath, Password: password}, engine, "")
		if checkpointErr != nil {
			return result, fmt.Errorf("maintenance committed but recovery checkpoint publication is pending: %w", checkpointErr)
		}
	}
	return result, nil
}
