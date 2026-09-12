package main

import (
	"fmt"
	"github.com/spf13/cobra"
	"p2pbk/internal/recovery"
	"p2pbk/internal/secrets"
)

func newRecoveryCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "recovery", Short: "Export or import an encrypted remote-repository recovery checkpoint"}
	cmd.AddCommand(&cobra.Command{Use: "export <bundle>", Args: cobra.ExactArgs(1), Short: "Export current metadata; keep and refresh this bundle off the source computer", RunE: func(cmd *cobra.Command, args []string) error {
		return recovery.Export(cmd.Context(), currentConfig(), secrets.FromEnv(), args[0])
	}})
	cmd.AddCommand(&cobra.Command{Use: "import <bundle> <new-data-dir>", Args: cobra.ExactArgs(2), Short: "Recover ownership and metadata into a new directory", RunE: func(cmd *cobra.Command, args []string) error {
		path, err := recovery.Import(cmd.Context(), args[0], secrets.FromEnv(), args[1])
		if err == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Recovered metadata: %s\nRun p2pbk --config %q repo init, then restore a snapshot.\n", path, path)
		}
		return err
	}})
	return cmd
}
