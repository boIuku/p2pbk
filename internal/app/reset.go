package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"p2pbk/internal/config"
)

const DeleteProtectedDataConfirmation = "DELETE-PROTECTED-DATA"

type ResetRequest struct {
	Config                     config.Config
	ConfigPath                 string
	Yes                        bool
	DataDir                    string
	DeleteProtectedData        bool
	ConfirmDeleteProtectedData string
}

type ResetResult struct {
	Removed []string
}

func ResetLocalState(req ResetRequest) (ResetResult, error) {
	if !req.Yes {
		return ResetResult{}, errors.New("refusing to reset without confirmation")
	}
	cfg := req.Config
	if req.DataDir != "" {
		cfg.Node.DataDir = req.DataDir
	}
	paths, err := cfg.Paths()
	if err != nil {
		return ResetResult{}, err
	}
	dataDir, err := filepath.Abs(paths.DataDir)
	if err != nil {
		return ResetResult{}, fmt.Errorf("resolve data dir: %w", err)
	}
	dataDir = filepath.Clean(dataDir)
	if err := validateResetTarget(dataDir); err != nil {
		return ResetResult{}, err
	}
	if err := enforceStorageResetPolicy(paths.StoreDir, dataDir, cfg.Backup.Roots, req); err != nil {
		return ResetResult{}, err
	}

	result := ResetResult{}
	if req.DeleteProtectedData {
		if req.ConfirmDeleteProtectedData != DeleteProtectedDataConfirmation {
			return ResetResult{}, fmt.Errorf("refusing to delete protected data without confirmation %s", DeleteProtectedDataConfirmation)
		}
		for _, root := range uniqueCleanPaths(cfg.Backup.Roots) {
			if isUnder(root, dataDir) {
				continue
			}
			if err := validateResetTarget(root); err != nil {
				return ResetResult{}, fmt.Errorf("protected root %s: %w", root, err)
			}
			if err := os.RemoveAll(root); err != nil {
				return ResetResult{}, fmt.Errorf("remove protected root %s: %w", root, err)
			}
			result.Removed = append(result.Removed, root)
		}
	}

	if err := os.RemoveAll(dataDir); err != nil {
		return ResetResult{}, fmt.Errorf("remove data dir %s: %w", dataDir, err)
	}
	result.Removed = append(result.Removed, dataDir)
	if req.ConfigPath != "" {
		absConfig, err := filepath.Abs(req.ConfigPath)
		if err != nil {
			return ResetResult{}, fmt.Errorf("resolve config path: %w", err)
		}
		absConfig = filepath.Clean(absConfig)
		if !isUnder(absConfig, dataDir) {
			if err := os.Remove(absConfig); err != nil && !errors.Is(err, os.ErrNotExist) {
				return ResetResult{}, fmt.Errorf("remove config %s: %w", absConfig, err)
			}
			result.Removed = append(result.Removed, absConfig)
		}
	}
	return result, nil
}

func enforceStorageResetPolicy(storeDir, dataDir string, roots []string, req ResetRequest) error {
	storeHasData, err := dirHasEntries(storeDir)
	if err != nil {
		return fmt.Errorf("inspect store dir %s: %w", storeDir, err)
	}
	if !storeHasData || len(protectedRootsOutsideData(roots, dataDir)) == 0 {
		return nil
	}
	if !req.DeleteProtectedData {
		return fmt.Errorf("refusing to reset non-empty storage while keeping protected roots; confirm protected data deletion with %s", DeleteProtectedDataConfirmation)
	}
	return nil
}

func protectedRootsOutsideData(roots []string, dataDir string) []string {
	out := []string{}
	for _, root := range uniqueCleanPaths(roots) {
		if !isUnder(root, dataDir) {
			out = append(out, root)
		}
	}
	return out
}

func dirHasEntries(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

func validateResetTarget(path string) error {
	if path == "" {
		return errors.New("reset data dir is required")
	}
	volume := filepath.VolumeName(path)
	if path == volume+string(os.PathSeparator) || path == string(os.PathSeparator) {
		return fmt.Errorf("refusing to remove filesystem root: %s", path)
	}
	home, _ := os.UserHomeDir()
	if samePath(path, home) {
		return fmt.Errorf("refusing to remove home directory: %s", path)
	}
	cwd, _ := os.Getwd()
	if samePath(path, cwd) {
		return fmt.Errorf("refusing to remove current working directory: %s", path)
	}
	return nil
}

func uniqueCleanPaths(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		abs, err := filepath.Abs(value)
		if err != nil {
			continue
		}
		clean := filepath.Clean(abs)
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
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

func isUnder(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
