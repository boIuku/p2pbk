package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"p2pbk/internal/config"
	"p2pbk/internal/kopiaengine"
)

type RepositoryStatus struct {
	ConfigPath    string
	StorageType   string
	SnapshotCount int
	UniqueID      string
}

type MaintenanceStatus struct {
	NextFull  time.Time
	NextQuick time.Time
}

func EnsureRepository(ctx context.Context, cfg config.Config, configPath, password string) (string, error) {
	if configPath == "" {
		return "", errors.New("p2pbk config file is required; pass --config or run p2pbk init first")
	}
	if password == "" {
		return "", errors.New("p2pbk password is required; enter it in the TUI or set P2PBK_REPOSITORY_PASSWORD")
	}
	engine := kopiaengine.New(configPath, cfg, password)
	if err := engine.EnsureRepository(ctx); err != nil {
		return "", err
	}
	return engine.RepositoryConfigPath(), nil
}

func GetRepositoryStatus(ctx context.Context, cfg config.Config, configPath, password string) (RepositoryStatus, error) {
	engine, err := existingEngine(cfg, configPath, password)
	if err != nil {
		return RepositoryStatus{}, err
	}
	status, err := engine.Status(ctx)
	if err != nil {
		return RepositoryStatus{}, friendlyRepositoryError(err)
	}
	out := RepositoryStatus{
		ConfigPath:    status.ConfigPath,
		StorageType:   status.StorageType,
		SnapshotCount: status.SnapshotCount,
	}
	if len(status.UniqueID) > 0 {
		out.UniqueID = hex.EncodeToString(status.UniqueID)
	}
	return out, nil
}

func GetMaintenanceStatus(ctx context.Context, cfg config.Config, configPath, password string) (MaintenanceStatus, error) {
	engine, err := existingEngine(cfg, configPath, password)
	if err != nil {
		return MaintenanceStatus{}, err
	}
	status, err := engine.MaintenanceStatus(ctx)
	if err != nil {
		return MaintenanceStatus{}, friendlyRepositoryError(err)
	}
	return MaintenanceStatus{NextFull: status.NextFull, NextQuick: status.NextQuick}, nil
}

func existingEngine(cfg config.Config, configPath, password string) (*kopiaengine.Engine, error) {
	if configPath == "" {
		return nil, errors.New("p2pbk config file is required; pass --config or run p2pbk init first")
	}
	probe := kopiaengine.New(configPath, cfg, "")
	if !probe.RepositoryConnected() {
		return nil, friendlyRepositoryError(kopiaengine.ErrRepositoryNotConnected)
	}
	if password == "" {
		return nil, errors.New("p2pbk password is required; enter it in the TUI or set P2PBK_REPOSITORY_PASSWORD")
	}
	return kopiaengine.New(configPath, cfg, password), nil
}

func friendlyRepositoryError(err error) error {
	if errors.Is(err, kopiaengine.ErrRepositoryNotConnected) {
		return fmt.Errorf("backup store is not initialized yet; open the TUI Settings tab and press i before backing up")
	}
	return err
}
