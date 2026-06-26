package main

import (
	"github.com/spf13/cobra"

	"p2pbk/internal/secrets"
	"p2pbk/internal/tui"
)

func newTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive p2pbk dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := currentConfig()
			return tui.Run(cfg, currentConfigPath(cfg), secrets.FromEnv())
		},
	}
}
