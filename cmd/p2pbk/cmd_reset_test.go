package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"p2pbk/internal/app"
	"p2pbk/internal/config"
)

func TestRunResetRequiresConfirmation(t *testing.T) {
	cfg := config.Default()
	cfg.Node.DataDir = filepath.Join(t.TempDir(), "node")

	err := runReset(cfg, "", resetOptions{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("runReset returned nil error without --yes")
	}
}

func TestRunResetRemovesDataDir(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "node")
	protectedDir := filepath.Join(root, "protected")
	if err := os.MkdirAll(filepath.Join(dataDir, "store"), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.MkdirAll(protectedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll protected returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "cache"), 0o700); err != nil {
		t.Fatalf("MkdirAll cache returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "cache", "x"), []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(protectedDir, "file.txt"), []byte("user-data"), 0o600); err != nil {
		t.Fatalf("WriteFile protected returned error: %v", err)
	}

	cfg := config.Default()
	cfg.Node.DataDir = dataDir
	cfg.Backup.Roots = []string{protectedDir}
	var out bytes.Buffer
	if err := runReset(cfg, "", resetOptions{yes: true}, &out); err != nil {
		t.Fatalf("runReset returned error: %v", err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("data dir still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(protectedDir); err != nil {
		t.Fatalf("protected data should remain by default: %v", err)
	}
}

func TestRunResetRemovesExternalConfig(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "node")
	configPath := filepath.Join(root, "config.json")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("MkdirAll data returned error: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile config returned error: %v", err)
	}

	cfg := config.Default()
	cfg.Node.DataDir = dataDir
	if err := runReset(cfg, configPath, resetOptions{yes: true}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runReset returned error: %v", err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("config still exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunResetRequiresExtraConfirmationForProtectedData(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "node")
	protectedDir := filepath.Join(root, "protected")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("MkdirAll data returned error: %v", err)
	}
	if err := os.MkdirAll(protectedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll protected returned error: %v", err)
	}

	cfg := config.Default()
	cfg.Node.DataDir = dataDir
	cfg.Backup.Roots = []string{protectedDir}
	err := runReset(cfg, "", resetOptions{yes: true, deleteProtectedData: true}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("runReset returned nil without protected data confirmation")
	}
	if _, statErr := os.Stat(protectedDir); statErr != nil {
		t.Fatalf("protected data should remain after refused reset: %v", statErr)
	}
}

func TestRunResetRefusesNonEmptyStoreWithoutProtectedDataDeletion(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "node")
	protectedDir := filepath.Join(root, "protected")
	storeDir := filepath.Join(dataDir, "store")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll store returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, "shard"), []byte("peer-shard"), 0o600); err != nil {
		t.Fatalf("WriteFile store returned error: %v", err)
	}
	if err := os.MkdirAll(protectedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll protected returned error: %v", err)
	}

	cfg := config.Default()
	cfg.Node.DataDir = dataDir
	cfg.Storage.StoreDir = "store"
	cfg.Backup.Roots = []string{protectedDir}
	err := runReset(cfg, "", resetOptions{yes: true}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("runReset returned nil while keeping protected data and deleting non-empty store")
	}
	if _, statErr := os.Stat(dataDir); statErr != nil {
		t.Fatalf("data dir should remain after refused reset: %v", statErr)
	}
	if _, statErr := os.Stat(protectedDir); statErr != nil {
		t.Fatalf("protected data should remain after refused reset: %v", statErr)
	}
}

func TestRunResetDeletesProtectedDataWithConfirmation(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "node")
	protectedDir := filepath.Join(root, "protected")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("MkdirAll data returned error: %v", err)
	}
	if err := os.MkdirAll(protectedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll protected returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(protectedDir, "file.txt"), []byte("user-data"), 0o600); err != nil {
		t.Fatalf("WriteFile protected returned error: %v", err)
	}

	cfg := config.Default()
	cfg.Node.DataDir = dataDir
	cfg.Backup.Roots = []string{protectedDir}
	err := runReset(cfg, "", resetOptions{
		yes:                        true,
		deleteProtectedData:        true,
		confirmDeleteProtectedData: app.DeleteProtectedDataConfirmation,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runReset returned error: %v", err)
	}
	if _, err := os.Stat(protectedDir); !os.IsNotExist(err) {
		t.Fatalf("protected dir still exists or stat failed unexpectedly: %v", err)
	}
}

func TestValidateResetTargetRejectsCurrentWorkingDirectory(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	cfg := config.Default()
	cfg.Node.DataDir = cwd
	if err := runReset(cfg, "", resetOptions{yes: true}, &bytes.Buffer{}); err == nil {
		t.Fatal("runReset returned nil for cwd")
	}
}
