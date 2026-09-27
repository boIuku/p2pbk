package app

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"p2pbk/internal/config"
)

type CacheBudgetError struct {
	Current int64
	Limit   int64
}

func (e *CacheBudgetError) Error() string {
	return fmt.Sprintf("cache budget exceeded: bytes=%d, limit=%d; unable to evict enough cache data", e.Current, e.Limit)
}

// EnforceCacheBudget evicts oldest regular cache files until the configured
// product-owned cache is within budget. It never follows symlinks or touches
// source roots, store data, manifests, or recovery material.
func EnforceCacheBudget(ctx context.Context, cfg config.Config) error {
	if cfg.Storage.CacheSizeBytes <= 0 {
		return nil
	}
	paths, err := cfg.Paths()
	if err != nil {
		return err
	}
	entries := []cacheEntry{}
	var total int64
	err = filepath.WalkDir(paths.CacheDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			return filepath.SkipDir
		}
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		total += info.Size()
		entries = append(entries, cacheEntry{path: path, size: info.Size(), modTime: info.ModTime()})
		return nil
	})
	if err != nil {
		return fmt.Errorf("inspect cache budget: %w", err)
	}
	if total <= cfg.Storage.CacheSizeBytes {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].modTime.Equal(entries[j].modTime) {
			return entries[i].path < entries[j].path
		}
		return entries[i].modTime.Before(entries[j].modTime)
	})
	for _, entry := range entries {
		if total <= cfg.Storage.CacheSizeBytes {
			break
		}
		if err := os.Remove(entry.path); err != nil {
			return &CacheBudgetError{Current: total, Limit: cfg.Storage.CacheSizeBytes}
		}
		total -= entry.size
	}
	if total > cfg.Storage.CacheSizeBytes {
		return &CacheBudgetError{Current: total, Limit: cfg.Storage.CacheSizeBytes}
	}
	return nil
}

type cacheEntry struct {
	path    string
	size    int64
	modTime time.Time
}
