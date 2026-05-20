package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"p2pbk/internal/backend/p2p/erasure"
	"p2pbk/internal/config"
	"p2pbk/internal/peer/blobserver"
)

type addOptions struct {
	path string
}

func newAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Add files or directories for backup",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: p2pbk add <path> [-- config-path <path>]")
			}

			cfg := config.FromViper(viper.GetViper())

			paths, err := cfg.Paths()
			if err != nil {
				return err
			}

			// Check if path exists
			if _, err := os.Stat(args[0]); os.IsNotExist(err) {
				return fmt.Errorf("path %q does not exist", args[0])
			}

			// Initialize local blobserver for storing shards
			if _, err := blobserver.New(paths.StoreDir); err != nil {
				return fmt.Errorf("initialize blobserver: %w", err)
			}
			// Check backup options
			backupCfg := cfg.Backup
			if backupCfg.DataShards == 0 {
				return fmt.Errorf("data_shards must be set in config.yaml")
			}
			if backupCfg.ParityShards == 0 {
				return fmt.Errorf("parity_shards must be set in config.yaml")
			}

			// Create codec for erasure coding
			_, err = erasure.New(backupCfg.DataShards, backupCfg.ParityShards)
			if err != nil {
				return fmt.Errorf("create codec: %w", err)
			}

			fmt.Printf("Added %s for backup\n", args[0])
			fmt.Printf("  data_shards: %d\n", backupCfg.DataShards)
			fmt.Printf("  parity_shards: %d\n", backupCfg.ParityShards)

			return nil
		},
	}

	return cmd
}
