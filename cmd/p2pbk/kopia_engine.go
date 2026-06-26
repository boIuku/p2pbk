package main

import (
	"fmt"
	"os"
	"path/filepath"

	"p2pbk/internal/config"
	"p2pbk/internal/kopiaengine"
	"p2pbk/internal/secrets"
)

func openKopiaEngine() (*kopiaengine.Engine, error) {
	cfg := currentConfig()
	configPath := currentConfigPath(cfg)
	if configPath == "" {
		return nil, fmt.Errorf("p2pbk config file is required for Kopia-backed backup; pass --config or run p2pbk init first")
	}
	password := secrets.FromEnv()
	if password == "" {
		return nil, fmt.Errorf("p2pbk password is required; set P2PBK_REPOSITORY_PASSWORD")
	}
	return kopiaengine.New(configPath, cfg, password), nil
}

func openExistingKopiaEngine() (*kopiaengine.Engine, error) {
	cfg := currentConfig()
	configPath := currentConfigPath(cfg)
	if configPath == "" {
		return nil, fmt.Errorf("p2pbk config file is required; pass --config or run p2pbk init first")
	}
	probe := kopiaengine.New(configPath, cfg, "")
	if !probe.RepositoryConnected() {
		return nil, fmt.Errorf("%w: run `p2pbk repo init` or `p2pbk backup now` first", kopiaengine.ErrRepositoryNotConnected)
	}
	password := secrets.FromEnv()
	if password == "" {
		return nil, fmt.Errorf("p2pbk password is required; set P2PBK_REPOSITORY_PASSWORD")
	}
	return kopiaengine.New(configPath, cfg, password), nil
}

func currentConfigPath(cfg config.Config) string {
	if used := configFileUsed(); used != "" {
		return used
	}
	paths, err := cfg.Paths()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(paths.DataDir, "config.json")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
