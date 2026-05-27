package main

import (
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
		Short: "Backup data from source to p2pbk",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: p2pbk backup <source> <target>")
			}
			repo, err := openLocalRepository(currentConfig())
			if err != nil {
				return err
			}
			idx, err := repo.Backup(args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "backed up %d file(s) as %s\n", len(idx.Entries), idx.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.source, "from", "", "source path (default: args[0])")
	cmd.Flags().StringVar(&opts.target, "to", "", "target path (default: args[1])")

	return cmd
}
