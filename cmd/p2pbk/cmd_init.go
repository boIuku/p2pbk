package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"p2pbk/internal/config"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/trust/identity"
)

type initOptions struct {
	dataDir        string
	backupDir      string
	maxSpace       string
	offerSpace     string
	schedule       string
	retention      string
	configPath     string
	passphrase     string
	start          bool
	displayName    string
	bootstrapPeers []string
}

func newInitCommand() *cobra.Command {
	opts := initOptions{
		dataDir:    config.Default().Node.DataDir,
		maxSpace:   "10GB",
		offerSpace: "10GB",
		schedule:   "daily",
		retention:  "30d",
		start:      true,
	}

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize p2pbk and start background backup service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.passphrase == "" {
				opts.passphrase = os.Getenv("P2PBK_IDENTITY_PASSPHRASE")
			}
			if opts.configPath == "" {
				opts.configPath = configFileUsed()
			}
			return runInit(opts, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&opts.dataDir, "data-dir", opts.dataDir, "node data directory")
	cmd.Flags().StringVar(&opts.backupDir, "dir", "", "directory to protect")
	cmd.Flags().StringVar(&opts.maxSpace, "max-space", opts.maxSpace, "maximum source data to protect, such as 10GB")
	cmd.Flags().StringVar(&opts.offerSpace, "offer-space", opts.offerSpace, "maximum local storage to offer peers, such as 20GB")
	cmd.Flags().StringVar(&opts.schedule, "schedule", opts.schedule, "backup policy schedule")
	cmd.Flags().StringVar(&opts.retention, "retention", opts.retention, "snapshot retention policy")
	cmd.Flags().StringVar(&opts.displayName, "name", "", "human-readable node name")
	cmd.Flags().StringSliceVar(&opts.bootstrapPeers, "bootstrap-peer", nil, "bootstrap peer multiaddr; repeat for multiple peers")
	cmd.Flags().StringVar(&opts.passphrase, "passphrase", "", "identity encryption passphrase")
	cmd.Flags().BoolVar(&opts.start, "start", opts.start, "start p2pbk in the background after initialization")

	return cmd
}

func runInit(opts initOptions, out io.Writer) error {
	if opts.passphrase == "" {
		return errors.New("identity passphrase is required; use --passphrase or P2PBK_IDENTITY_PASSPHRASE")
	}

	cfg := config.Default()
	cfg.Node.DataDir = opts.dataDir
	cfg.Node.DisplayName = opts.displayName
	cfg.Storage.StoreDir = "store"
	cfg.Storage.CacheDir = "cache"
	cfg.Backup.Schedule = opts.schedule
	cfg.Backup.Retention = opts.retention
	cfg.Backup.InitialFull = true
	cfg.Backup.Incremental = true
	cfg.Network.BootstrapPeers = opts.bootstrapPeers

	maxBytes, err := parseByteSize(opts.maxSpace)
	if err != nil {
		return fmt.Errorf("parse --max-space: %w", err)
	}
	offerBytes, err := parseByteSize(opts.offerSpace)
	if err != nil {
		return fmt.Errorf("parse --offer-space: %w", err)
	}
	cfg.Backup.MaxBytes = maxBytes
	cfg.Storage.OfferBytes = offerBytes
	if opts.backupDir != "" {
		cfg.Backup.Roots = []string{opts.backupDir}
	}

	paths, err := cfg.Paths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.StoreDir, 0o700); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	if err := os.MkdirAll(paths.CacheDir, 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}

	id, err := identity.Create(paths.Identity, opts.passphrase)
	if err != nil {
		return fmt.Errorf("create identity: %w", err)
	}

	db, err := localdb.Open(paths.LocalDB)
	if err != nil {
		return err
	}
	if err := db.PutSetting("identity_fingerprint", []byte(id.Fingerprint())); err != nil {
		_ = db.Close()
		return err
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("close localdb: %w", err)
	}

	configPath := opts.configPath
	if configPath == "" {
		configPath = filepath.Join(paths.DataDir, "config.json")
	}
	if err := writeInitialConfig(configPath, cfg); err != nil {
		return err
	}

	fmt.Fprintf(out, "initialized p2pbk node at %s\n", paths.DataDir)
	fmt.Fprintf(out, "config: %s\n", configPath)
	fmt.Fprintf(out, "identity fingerprint: %s\n", id.Fingerprint())
	fmt.Fprintf(out, "protecting up to %s from %d director%s\n", formatBytes(cfg.Backup.MaxBytes), len(cfg.Backup.Roots), plural(len(cfg.Backup.Roots)))
	fmt.Fprintf(out, "network storage required for that policy: about %s\n", formatBytes(erasureStorageBytes(cfg.Backup.MaxBytes, cfg.Backup.DataShards, cfg.Backup.ParityShards)))
	fmt.Fprintf(out, "offering up to %s to other peers\n", formatBytes(cfg.Storage.OfferBytes))
	if opts.start {
		if err := startDaemon(configPath, opts.passphrase); err != nil {
			return err
		}
		fmt.Fprintln(out, "p2pbk is running in the background")
	}
	return nil
}

func writeInitialConfig(path string, cfg config.Config) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	content = append(content, '\n')
	return os.WriteFile(path, []byte(content), 0o600)
}

func parseByteSize(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, errors.New("size is required")
	}
	upper := strings.ToUpper(trimmed)
	multipliers := []struct {
		suffix string
		value  int64
	}{
		{"TIB", 1 << 40},
		{"TB", 1_000_000_000_000},
		{"GIB", 1 << 30},
		{"GB", 1_000_000_000},
		{"MIB", 1 << 20},
		{"MB", 1_000_000},
		{"KIB", 1 << 10},
		{"KB", 1_000},
		{"B", 1},
	}
	for _, m := range multipliers {
		if strings.HasSuffix(upper, m.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSpace(trimmed[:len(trimmed)-len(m.suffix)]), 64)
			if err != nil {
				return 0, err
			}
			if n < 0 {
				return 0, errors.New("size must be non-negative")
			}
			return int64(n * float64(m.value)), nil
		}
	}
	n, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, errors.New("size must be non-negative")
	}
	return n, nil
}

func formatBytes(value int64) string {
	const gb = int64(1_000_000_000)
	if value >= gb && value%gb == 0 {
		return fmt.Sprintf("%dGB", value/gb)
	}
	return fmt.Sprintf("%d bytes", value)
}

func erasureStorageBytes(sourceBytes int64, dataShards, parityShards int) int64 {
	if sourceBytes <= 0 || dataShards <= 0 {
		return 0
	}
	return sourceBytes * int64(dataShards+parityShards) / int64(dataShards)
}

func plural(count int) string {
	if count == 1 {
		return "y"
	}
	return "ies"
}

func startDaemon(configPath, identityPassphrase string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	logPath := filepath.Join(filepath.Dir(configPath), "p2pbk.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(exe, "--config", configPath, "daemon")
	if identityPassphrase != "" {
		cmd.Env = append(os.Environ(), "P2PBK_IDENTITY_PASSPHRASE="+identityPassphrase)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start background p2pbk: %w", err)
	}
	return cmd.Process.Release()
}
