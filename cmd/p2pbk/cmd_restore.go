package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

type restoreOptions struct {
	source string
	target string
}

func newRestoreCommand() *cobra.Command {
	opts := restoreOptions{}

	cmd := &cobra.Command{
		Use:   "restore <source> <target>",
		Short: "Restore data from p2pbk to target",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: p2pbk restore <source> <target>")
			}
			repo, err := openLocalRepository(currentConfig())
			if err != nil {
				return err
			}
			idx, err := repo.Restore(args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "restored %d file(s) from %s\n", len(idx.Entries), idx.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.source, "from", "", "source path (default: args[0])")
	cmd.Flags().StringVar(&opts.target, "to", "", "target path (default: args[1])")

	return cmd
}
