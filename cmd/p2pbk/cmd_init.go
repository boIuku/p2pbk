package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"p2pbk/internal/config"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/trust/identity"
)

type initOptions struct {
	dataDir    string
	configPath string
	passphrase string
}

func newInitCommand() *cobra.Command {
	opts := initOptions{
		dataDir: config.Default().Node.DataDir,
	}

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a p2pbk node",
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.passphrase == "" {
				opts.passphrase = os.Getenv("P2PBK_IDENTITY_PASSPHRASE")
			}
			return runInit(opts, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&opts.dataDir, "data-dir", opts.dataDir, "node data directory")
	cmd.Flags().StringVar(&opts.configPath, "config", "", "config file to create")
	cmd.Flags().StringVar(&opts.passphrase, "passphrase", "", "identity encryption passphrase")

	return cmd
}

func runInit(opts initOptions, out io.Writer) error {
	if opts.passphrase == "" {
		return errors.New("identity passphrase is required; use --passphrase or P2PBK_IDENTITY_PASSPHRASE")
	}

	cfg := config.Default()
	cfg.Node.DataDir = opts.dataDir
	cfg.Storage.StoreDir = "store"
	cfg.Storage.CacheDir = "cache"

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
		configPath = filepath.Join(paths.DataDir, "config.yaml")
	}
	if err := writeInitialConfig(configPath, cfg); err != nil {
		return err
	}

	fmt.Fprintf(out, "initialized p2pbk node at %s\n", paths.DataDir)
	fmt.Fprintf(out, "identity fingerprint: %s\n", id.Fingerprint())
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
	content := fmt.Sprintf(`node:
  data_dir: %s
  identity: %s
  display_name: %q

storage:
  store_dir: %s
  cache_dir: %s
  cache_size_bytes: %d
  offer_bytes: %d

network:
  listen_addrs:
    - /ip4/0.0.0.0/tcp/0
    - /ip4/0.0.0.0/udp/0/quic-v1
  bootstrap_peers: []
  protocol_id: %s
  min_peers: %d

backup:
  data_shards: %d
  parity_shards: %d
  roots: []

trust:
  challenge_interval: %s
  reputation_window: %s
`,
		cfg.Node.DataDir,
		cfg.Node.Identity,
		cfg.Node.DisplayName,
		cfg.Storage.StoreDir,
		cfg.Storage.CacheDir,
		cfg.Storage.CacheSizeBytes,
		cfg.Storage.OfferBytes,
		cfg.Network.ProtocolID,
		cfg.Network.MinPeers,
		cfg.Backup.DataShards,
		cfg.Backup.ParityShards,
		cfg.Trust.ChallengeInterval,
		cfg.Trust.ReputationWindow,
	)
	return os.WriteFile(path, []byte(content), 0o600)
}
