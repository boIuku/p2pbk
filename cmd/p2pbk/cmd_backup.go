package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"p2pbk/internal/app"
	"p2pbk/internal/secrets"
)

type backupOptions struct {
	source string
	target string
}

func newBackupCommand() *cobra.Command {
	opts := backupOptions{}

	cmd := &cobra.Command{
		Use:   "backup now|<source> <description>",
		Short: "Run an on-demand backup",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := currentConfig()
			configPath := currentConfigPath(cfg)
			password := secrets.FromEnv()
			if len(args) == 1 && args[0] == "now" {
				results, err := app.BackupAll(context.Background(), cfg, configPath, password, "manual")
				for _, result := range results {
					fmt.Fprintf(cmd.OutOrStdout(), "snapshot %s saved for %s\n", result.ManifestID, result.Source)
					if result.CheckpointState == "pending" {
						fmt.Fprintf(cmd.OutOrStdout(), "  recovery checkpoint pending: %s\n", result.CheckpointError)
					}
				}
				return err
			}
			if len(args) < 2 {
				return fmt.Errorf("usage: p2pbk backup now OR p2pbk backup <source> <description>")
			}
			result, err := app.BackupRoot(context.Background(), app.BackupRequest{
				Config:      cfg,
				ConfigPath:  configPath,
				Password:    password,
				Source:      args[0],
				Description: args[1],
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "snapshot %s saved for %s\n", result.ManifestID, result.Source)
			if result.CheckpointState == "pending" {
				fmt.Fprintf(cmd.OutOrStdout(), "recovery checkpoint pending: %s\n", result.CheckpointError)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.source, "from", "", "source path (default: args[0])")
	cmd.Flags().StringVar(&opts.target, "to", "", "target path (default: args[1])")

	return cmd
}
