package config

import (
	"path/filepath"
	"testing"
)

func TestPathsResolveRelativeToDataDir(t *testing.T) {
	cfg := Default()
	cfg.Node.DataDir = filepath.Join("tmp", "p2pbk")
	cfg.Node.Identity = "id.age"
	cfg.Storage.StoreDir = "store"
	cfg.Storage.CacheDir = "cache"

	paths, err := cfg.Paths()
	if err != nil {
		t.Fatalf("Paths returned error: %v", err)
	}

	if paths.Identity != filepath.Clean(filepath.Join("tmp", "p2pbk", "id.age")) {
		t.Fatalf("identity path = %q", paths.Identity)
	}
	if paths.LocalDB != filepath.Clean(filepath.Join("tmp", "p2pbk", "local.db")) {
		t.Fatalf("local db path = %q", paths.LocalDB)
	}
}
