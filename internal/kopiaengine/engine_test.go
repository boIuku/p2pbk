package kopiaengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"p2pbk/internal/config"
)

func TestEngineBackupRestoreLocalP2PBKStorage(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dataDir := filepath.Join(root, "node")
	configPath := filepath.Join(dataDir, "config.yaml")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	writeTestConfig(t, configPath, dataDir)

	src := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o700); err != nil {
		t.Fatalf("MkdirAll src returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "file.txt"), []byte("kopia over p2pbk"), 0o600); err != nil {
		t.Fatalf("WriteFile source returned error: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load config returned error: %v", err)
	}
	engine := New(configPath, cfg, "repo-password")
	backup, err := engine.Backup(ctx, src, "test snapshot")
	if err != nil {
		t.Fatalf("Backup returned error: %v", err)
	}
	if backup.ManifestID == "" {
		t.Fatal("Backup returned empty manifest ID")
	}

	dst := filepath.Join(root, "restore")
	restore, err := engine.Restore(ctx, "latest", dst)
	if err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	if restore.ManifestID == "" {
		t.Fatal("Restore returned empty manifest ID")
	}
	restoredPath := findFile(t, dst, "file.txt")
	got, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("ReadFile restored returned error: %v", err)
	}
	if string(got) != "kopia over p2pbk" {
		t.Fatalf("restored data = %q", got)
	}
}

func TestRestoreRefusesOverwriteByDefault(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dataDir := filepath.Join(root, "node")
	configPath := filepath.Join(dataDir, "config.yaml")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestConfig(t, configPath, dataDir)
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "source")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := New(configPath, cfg, "repo-password")
	if _, err := engine.Backup(ctx, src, "restore safety"); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "restore")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "existing.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Restore(ctx, "latest", dst); err == nil {
		t.Fatal("restore overwrote a non-empty destination by default")
	}
	data, err := os.ReadFile(filepath.Join(dst, "existing.txt"))
	if err != nil || string(data) != "keep" {
		t.Fatalf("refused restore changed destination: %q %v", data, err)
	}
	if _, err := engine.RestoreWithOptions(ctx, "latest", dst, RestoreOptions{AllowOverwrite: true}, nil); err != nil {
		t.Fatalf("explicit overwrite restore failed: %v", err)
	}
}

func findFile(t *testing.T, root, name string) string {
	t.Helper()

	var found string
	var seen []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != name {
			if err == nil {
				seen = append(seen, path)
			}
			return err
		}
		found = path
		return filepath.SkipAll
	})
	if err != nil {
		t.Fatalf("WalkDir restored returned error: %v", err)
	}
	if found == "" {
		t.Fatalf("restored file %q not found under %s; saw %v", name, root, seen)
	}
	return found
}

func writeTestConfig(t *testing.T, path, dataDir string) {
	t.Helper()

	content := `node:
  data_dir: ` + filepath.ToSlash(dataDir) + `
  identity: identity.age
  display_name: test

storage:
  store_dir: store
  cache_dir: cache
  cache_size_bytes: 1073741824
  offer_bytes: 10737418240

network:
  listen_addrs:
    - /ip4/127.0.0.1/tcp/0
  bootstrap_peers: []
  protocol_id: /p2pbk/1.0.0
  min_peers: 0

backup:
  data_shards: 2
  parity_shards: 1
  roots: []

trust:
  challenge_interval: 1h
  reputation_window: 720h
  challenge_max_length: 65536
  identity_passphrase: ""
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile config returned error: %v", err)
	}
}
