package main

import (
	"fmt"
	"os"
	"path/filepath"

	"p2pbk/internal/config"
	"p2pbk/internal/kopiaengine"
)

func openKopiaEngine() (*kopiaengine.Engine, error) {
	cfg := currentConfig()
	configPath := currentConfigPath(cfg)
	if configPath == "" {
		return nil, fmt.Errorf("p2pbk config file is required for Kopia-backed backup; pass --config or run p2pbk init first")
	}
	password := os.Getenv("P2PBK_REPOSITORY_PASSWORD")
	if password == "" {
		return nil, fmt.Errorf("repository password is required; set P2PBK_REPOSITORY_PASSWORD")
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
