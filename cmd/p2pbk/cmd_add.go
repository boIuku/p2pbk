package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"p2pbk/internal/app"
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

			cfg, err := app.AddAsset(currentConfig(), configFileUsed(), args[0])
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added %s for backup\n", args[0])
			fmt.Fprintf(cmd.OutOrStdout(), "  data_shards: %d\n", cfg.Backup.DataShards)
			fmt.Fprintf(cmd.OutOrStdout(), "  parity_shards: %d\n", cfg.Backup.ParityShards)

			return nil
		},
	}

	return cmd
}
