package main

import (
	"os"

	"github.com/spf13/cobra"

	"p2pbk/internal/tui"
)

func newTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive p2pbk dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := currentConfig()
			return tui.Run(cfg, currentConfigPath(cfg), os.Getenv("P2PBK_REPOSITORY_PASSWORD"))
		},
	}
}
