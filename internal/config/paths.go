package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Paths struct {
	DataDir  string
	Identity string
	LocalDB  string
	StoreDir string
	CacheDir string
}

func Default() Config {
	cfg, err := Load("")
	if err != nil {
		panic(err)
	}
	return cfg
}

func (c Config) Paths() (*Paths, error) {
	dataDir, err := normalizePath(c.Node.DataDir)
	if err != nil {
		return nil, fmt.Errorf("node.data_dir: %w", err)
	}

	identity, err := normalizeUnder(dataDir, c.Node.Identity)
	if err != nil {
		return nil, fmt.Errorf("node.identity: %w", err)
	}
	storeDir, err := normalizeUnder(dataDir, c.Storage.StoreDir)
	if err != nil {
		return nil, fmt.Errorf("storage.store_dir: %w", err)
	}
	cacheDir, err := normalizeUnder(dataDir, c.Storage.CacheDir)
	if err != nil {
		return nil, fmt.Errorf("storage.cache_dir: %w", err)
	}

	return &Paths{
		DataDir:  dataDir,
		Identity: identity,
		LocalDB:  filepath.Join(dataDir, "local.db"),
		StoreDir: storeDir,
		CacheDir: cacheDir,
	}, nil
}

func normalizeUnder(base, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(value) || strings.HasPrefix(value, "~") {
		return normalizePath(value)
	}
	return filepath.Clean(filepath.Join(base, value)), nil
}

func normalizePath(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("path is required")
	}
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if value == "~" {
			return home, nil
		}
		value = filepath.Join(home, value[2:])
	}
	return filepath.Clean(value), nil
}
