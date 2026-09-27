package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"p2pbk/internal/config"
)

// ProtectedByteLimitError is returned before a backup can mutate the
// repository when the configured logical protected-data budget is exceeded.
// Required is the de-duplicated size of regular files in the configured roots.
type ProtectedByteLimitError struct {
	Limit    int64
	Required int64
}

func (e *ProtectedByteLimitError) Error() string {
	return fmt.Sprintf("protected byte limit exceeded: logical regular-file bytes=%d, limit=%d; remove a protected root or increase backup.max_bytes", e.Required, e.Limit)
}

// MeasureLogicalFileBytes counts regular files once across overlapping roots.
// Directory symlinks are not followed, which keeps the admission check finite
// and prevents a symlink from escaping a protected root.
func MeasureLogicalFileBytes(ctx context.Context, roots []string) (int64, error) {
	return measureLogicalFileBytes(ctx, roots, nil)
}

func enforceProtectedByteBudget(ctx context.Context, cfg config.Config, source string) error {
	if cfg.Backup.MaxBytes <= 0 {
		return nil
	}
	roots := NormalizeRootList(cfg.Backup.Roots)
	if len(roots) == 0 {
		roots = []string{source}
	} else if source != "" {
		seen := false
		for _, root := range roots {
			if samePath(root, source) {
				seen = true
				break
			}
		}
		if !seen {
			roots = append(roots, source)
		}
	}
	paths, err := cfg.Paths()
	if err != nil {
		return err
	}
	excludes := []string{paths.DataDir, paths.StoreDir, paths.CacheDir, paths.LocalDB, paths.Identity}
	bytes, err := measureLogicalFileBytes(ctx, roots, excludes)
	if err != nil {
		return err
	}
	if bytes > cfg.Backup.MaxBytes {
		return &ProtectedByteLimitError{Limit: cfg.Backup.MaxBytes, Required: bytes}
	}
	return nil
}

func measureLogicalFileBytes(ctx context.Context, roots, excludes []string) (int64, error) {
	seen := map[string]struct{}{}
	canonicalExcludes := make([]string, 0, len(excludes))
	for _, path := range excludes {
		if canonical, err := canonicalPath(path); err == nil {
			canonicalExcludes = append(canonicalExcludes, canonical)
		}
	}
	var total int64
	for _, root := range NormalizeRootList(roots) {
		canonicalRoot, err := canonicalPath(root)
		if err != nil {
			return 0, fmt.Errorf("resolve protected root %q: %w", root, err)
		}
		if pathExcluded(canonicalRoot, canonicalExcludes) {
			continue
		}
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("walk protected root %q at %q: %w", root, path, walkErr)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			canonical, err := canonicalPath(path)
			if err != nil {
				return fmt.Errorf("resolve protected path %q: %w", path, err)
			}
			if pathExcluded(canonical, canonicalExcludes) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("inspect protected file %q: %w", path, err)
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			key := canonicalKey(canonical)
			if _, ok := seen[key]; ok {
				return nil
			}
			seen[key] = struct{}{}
			total += info.Size()
			return nil
		}); err != nil {
			return 0, err
		}
	}
	return total, nil
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if _, statErr := os.Stat(abs); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		return "", err
	}
	return filepath.Clean(abs), nil
}

func canonicalKey(path string) string {
	if filepath.Separator == '\\' {
		return strings.ToLower(filepath.Clean(path))
	}
	return filepath.Clean(path)
}

func pathExcluded(path string, excludes []string) bool {
	for _, excluded := range excludes {
		if samePath(path, excluded) || sameOrChildProtectedPath(path, excluded) {
			return true
		}
	}
	return false
}

func sameOrChildProtectedPath(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}
