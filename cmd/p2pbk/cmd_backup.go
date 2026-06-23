package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
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
			engine, err := openKopiaEngine()
			if err != nil {
				return err
			}
			if len(args) == 1 && args[0] == "now" {
				cfg := currentConfig()
				if len(cfg.Backup.Roots) == 0 {
					return fmt.Errorf("no backup roots configured; run p2pbk init --dir <path> or set backup.roots")
				}
				for _, root := range cfg.Backup.Roots {
					result, err := engine.Backup(context.Background(), root, "manual")
					if err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "snapshot %s saved for %s\n", result.ManifestID, result.Source)
				}
				return nil
			}
			if len(args) < 2 {
				return fmt.Errorf("usage: p2pbk backup now OR p2pbk backup <source> <description>")
			}
			result, err := engine.Backup(context.Background(), args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "snapshot %s saved for %s\n", result.ManifestID, result.Source)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.source, "from", "", "source path (default: args[0])")
	cmd.Flags().StringVar(&opts.target, "to", "", "target path (default: args[1])")

	return cmd
}
