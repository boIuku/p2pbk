package app

import (
	"path/filepath"
	"testing"

	"p2pbk/internal/config"
)

func TestSplitPathListStripsWrappingQuotes(t *testing.T) {
	got := SplitPathList(`"C:\Users\Nazar\Documents\backupTest"`)
	if len(got) != 1 {
		t.Fatalf("SplitPathList returned %d path(s), want 1: %v", len(got), got)
	}
	want := `C:\Users\Nazar\Documents\backupTest`
	if got[0] != want {
		t.Fatalf("SplitPathList = %q, want %q", got[0], want)
	}
}

func TestSplitPathListStripsQuotesPerPath(t *testing.T) {
	got := SplitPathList(`"C:\one";'D:\two'`)
	want := []string{`C:\one`, `D:\two`}
	if len(got) != len(want) {
		t.Fatalf("SplitPathList returned %d path(s), want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SplitPathList[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRemoveAssetRemovesProtectedRoot(t *testing.T) {
	tmp := t.TempDir()
	one := filepath.Join(tmp, "one")
	two := filepath.Join(tmp, "two")
	cfg := config.Default()
	cfg.Node.DataDir = filepath.Join(tmp, "data")
	cfg.Backup.Roots = []string{one, two}
	configPath := filepath.Join(tmp, "config.json")

	got, err := RemoveAsset(cfg, configPath, one)
	if err != nil {
		t.Fatalf("RemoveAsset returned error: %v", err)
	}
	if len(got.Backup.Roots) != 1 || !samePath(got.Backup.Roots[0], two) {
		t.Fatalf("roots after remove = %#v, want only %q", got.Backup.Roots, two)
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if len(loaded.Backup.Roots) != 1 || !samePath(loaded.Backup.Roots[0], two) {
		t.Fatalf("saved roots after remove = %#v, want only %q", loaded.Backup.Roots, two)
	}
}
