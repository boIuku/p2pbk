package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"p2pbk/internal/config"
)

func TestCacheAndStagingRespectDiskBudgets(t *testing.T) {
	dataDir := t.TempDir()
	cfg := config.Default()
	cfg.Node.DataDir = dataDir
	cfg.Storage.CacheDir = "cache"
	cfg.Storage.CacheSizeBytes = 5
	paths, err := cfg.Paths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.CacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(paths.CacheDir, "old")
	newer := filepath.Join(paths.CacheDir, "newer")
	if err := os.WriteFile(old, []byte("old-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := EnforceCacheBudget(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old cache entry still exists: %v", err)
	}
	if _, err := os.Stat(newer); err != nil {
		t.Fatalf("newer cache entry was removed: %v", err)
	}
}
