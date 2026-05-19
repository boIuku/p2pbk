package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"p2pbk/internal/version"
)

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "p2pbk",
		Short: "Peer-to-peer backup CLI",
	}

	cmd.AddCommand(newInitCommand())
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print build version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version.String())
		},
	})

	return cmd
}
