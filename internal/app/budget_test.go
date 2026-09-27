package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"p2pbk/internal/config"
)

func TestProtectedByteLimitEnforcedDuringBackup(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload.bin"), []byte("1234567890"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Node.DataDir = filepath.Join(t.TempDir(), "state")
	cfg.Backup.Roots = []string{root}
	cfg.Backup.MaxBytes = 9

	err := enforceProtectedByteBudget(context.Background(), cfg, root)
	var limitErr *ProtectedByteLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want ProtectedByteLimitError", err)
	}
	if limitErr.Required != 10 || limitErr.Limit != 9 {
		t.Fatalf("limit error = %+v, want required 10 limit 9", limitErr)
	}
}

func TestProtectedByteLimitCountsOverlappingRootsOnce(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "payload.bin"), []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	bytes, err := MeasureLogicalFileBytes(context.Background(), []string{parent, child})
	if err != nil {
		t.Fatal(err)
	}
	if bytes != 4 {
		t.Fatalf("logical bytes = %d, want 4", bytes)
	}
}

func TestProtectedByteLimitSurfacesSourceReadFailure(t *testing.T) {
	cfg := config.Default()
	cfg.Node.DataDir = filepath.Join(t.TempDir(), "state")
	cfg.Backup.Roots = []string{filepath.Join(t.TempDir(), "missing")}
	cfg.Backup.MaxBytes = 1
	err := enforceProtectedByteBudget(context.Background(), cfg, cfg.Backup.Roots[0])
	if err == nil || !strings.Contains(err.Error(), "resolve protected root") {
		t.Fatalf("error = %v, want source read failure", err)
	}
}
