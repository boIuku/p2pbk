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
		Use:   "backup <source> <target>",
		Short: "Backup data from source to a Kopia repository on p2pbk storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: p2pbk backup <source> <target>")
			}
			engine, err := openKopiaEngine()
			if err != nil {
				return err
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
