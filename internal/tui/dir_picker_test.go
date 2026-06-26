package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirPickerCurrentPathUsesHighlightedRow(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	beta := filepath.Join(root, "beta")
	if err := os.MkdirAll(alpha, 0o700); err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if err := os.MkdirAll(beta, 0o700); err != nil {
		t.Fatalf("create beta: %v", err)
	}

	picker := newDirPicker("Select Path", root, 0, true)
	picker.resize(100, 30)
	picker.table.SetCursor(1)

	if got := picker.currentPath(); !sameCleanPath(got, beta) {
		t.Fatalf("currentPath = %q, want highlighted row %q", got, beta)
	}
	picker.toggle(picker.currentPath())
	values := picker.values()
	if len(values) != 1 || !sameCleanPath(values[0], beta) {
		t.Fatalf("values after toggle = %#v, want only %q", values, beta)
	}
}

func sameCleanPath(left, right string) bool {
	leftAbs, err := filepath.Abs(left)
	if err != nil {
		return false
	}
	rightAbs, err := filepath.Abs(right)
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}
