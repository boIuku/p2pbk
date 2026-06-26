package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"p2pbk/internal/app"
)

type restoreOptions struct {
	source string
	target string
}

func newRestoreCommand() *cobra.Command {
	opts := restoreOptions{}

	cmd := &cobra.Command{
		Use:   "restore <snapshot-id|latest> <target>",
		Short: "Restore a Kopia snapshot from p2pbk storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: p2pbk restore <snapshot-id|latest> <target>")
			}
			cfg := currentConfig()
			result, err := app.RestoreSnapshot(context.Background(), app.RestoreRequest{
				Config:     cfg,
				ConfigPath: currentConfigPath(cfg),
				Password:   os.Getenv("P2PBK_REPOSITORY_PASSWORD"),
				Snapshot:   args[0],
				Target:     args[1],
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "restored snapshot %s: %d file(s), %d dir(s)\n", result.ManifestID, result.Files, result.Dirs)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.source, "from", "", "source path (default: args[0])")
	cmd.Flags().StringVar(&opts.target, "to", "", "target path (default: args[1])")

	return cmd
}
