package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"p2pbk/internal/config"
)

const deleteProtectedDataConfirmation = "DELETE-PROTECTED-DATA"

type resetOptions struct {
	yes                        bool
	dataDir                    string
	deleteProtectedData        bool
	confirmDeleteProtectedData string
}

func newResetCommand() *cobra.Command {
	opts := resetOptions{}
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Remove local p2pbk state and config",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReset(currentConfig(), configFileUsed(), opts, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "confirm deletion of local p2pbk state")
	cmd.Flags().StringVar(&opts.dataDir, "data-dir", "", "data directory to remove; defaults to the active config's node.data_dir")
	cmd.Flags().BoolVar(&opts.deleteProtectedData, "delete-protected-data", false, "also delete configured backup roots")
	cmd.Flags().StringVar(&opts.confirmDeleteProtectedData, "confirm-delete-protected-data", "", "required value DELETE-PROTECTED-DATA when deleting configured backup roots")
	return cmd
}

func runReset(cfg config.Config, configPath string, opts resetOptions, out io.Writer) error {
	if !opts.yes {
		return errors.New("refusing to reset without --yes")
	}
	if opts.dataDir != "" {
		cfg.Node.DataDir = opts.dataDir
	}
	paths, err := cfg.Paths()
	if err != nil {
		return err
	}
	dataDir, err := filepath.Abs(paths.DataDir)
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	dataDir = filepath.Clean(dataDir)
	if err := validateResetTarget(dataDir); err != nil {
		return err
	}

	if err := enforceStorageResetPolicy(paths.StoreDir, dataDir, cfg.Backup.Roots, opts); err != nil {
		return err
	}
	if err := removeProtectedData(cfg.Backup.Roots, dataDir, opts, out); err != nil {
		return err
	}

	if err := os.RemoveAll(dataDir); err != nil {
		return fmt.Errorf("remove data dir %s: %w", dataDir, err)
	}
	fmt.Fprintf(out, "removed data dir: %s\n", dataDir)

	if configPath != "" {
		if err := removeExternalConfig(configPath, dataDir, out); err != nil {
			return err
		}
	}
	return nil
}

func enforceStorageResetPolicy(storeDir, dataDir string, roots []string, opts resetOptions) error {
	storeHasData, err := dirHasEntries(storeDir)
	if err != nil {
		return fmt.Errorf("inspect store dir %s: %w", storeDir, err)
	}
	if !storeHasData {
		return nil
	}
	externalRoots := protectedRootsOutsideData(roots, dataDir)
	if len(externalRoots) == 0 {
		return nil
	}
	if !opts.deleteProtectedData {
		return fmt.Errorf("refusing to reset non-empty storage while keeping protected roots; this node may be storing other peers' shards, so rerun with --delete-protected-data --confirm-delete-protected-data=%s", deleteProtectedDataConfirmation)
	}
	return nil
}

func removeProtectedData(roots []string, dataDir string, opts resetOptions, out io.Writer) error {
	roots = uniqueCleanPaths(roots)
	if len(roots) == 0 {
		return nil
	}
	if !opts.deleteProtectedData {
		fmt.Fprintf(out, "protected data left untouched: %d configured root%s\n", len(roots), plural(len(roots)))
		return nil
	}
	if opts.confirmDeleteProtectedData != deleteProtectedDataConfirmation {
		return fmt.Errorf("refusing to delete protected data without --confirm-delete-protected-data=%s", deleteProtectedDataConfirmation)
	}
	for _, root := range roots {
		if isUnder(root, dataDir) {
			continue
		}
		if err := validateResetTarget(root); err != nil {
			return fmt.Errorf("protected root %s: %w", root, err)
		}
		if err := os.RemoveAll(root); err != nil {
			return fmt.Errorf("remove protected root %s: %w", root, err)
		}
		fmt.Fprintf(out, "removed protected root: %s\n", root)
	}
	return nil
}

func protectedRootsOutsideData(roots []string, dataDir string) []string {
	roots = uniqueCleanPaths(roots)
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if isUnder(root, dataDir) {
			continue
		}
		out = append(out, root)
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

func removeExternalConfig(configPath, dataDir string, out io.Writer) error {
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	absConfig = filepath.Clean(absConfig)
	if isUnder(absConfig, dataDir) {
		return nil
	}
	if err := os.Remove(absConfig); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove config %s: %w", absConfig, err)
	}
	fmt.Fprintf(out, "removed config: %s\n", absConfig)
	return nil
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
