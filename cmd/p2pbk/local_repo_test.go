package main

import (
	"os"
	"path/filepath"
	"testing"

	"p2pbk/internal/config"
)

func TestLocalRepositoryBackupRestoreDirectory(t *testing.T) {
	cfg := config.Default()
	cfg.Node.DataDir = filepath.Join(t.TempDir(), "node")
	repo, err := openLocalRepository(cfg)
	if err != nil {
		t.Fatalf("openLocalRepository returned error: %v", err)
	}

	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "file.txt"), []byte("hello backup"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	idx, err := repo.Backup(src, "snapshot")
	if err != nil {
		t.Fatalf("Backup returned error: %v", err)
	}
	if len(idx.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(idx.Entries))
	}

	dst := filepath.Join(t.TempDir(), "restore")
	if _, err := repo.Restore("snapshot", dst); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "nested", "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(got) != "hello backup" {
		t.Fatalf("restored data = %q", got)
	}
}
