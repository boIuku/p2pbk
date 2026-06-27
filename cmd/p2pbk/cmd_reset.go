package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"p2pbk/internal/app"
	"p2pbk/internal/config"
)

type resetOptions struct {
	yes                        bool
	dataDir                    string
	deleteProtectedData        bool
	confirmDeleteProtectedData string
}

func newResetCommand() *cobra.Command {
	opts := resetOptions{}
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Remove local p2pbk state and config",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReset(currentConfig(), configFileUsed(), opts, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "confirm deletion of local p2pbk state")
	cmd.Flags().StringVar(&opts.dataDir, "data-dir", "", "data directory to remove; defaults to the active config's node.data_dir")
	cmd.Flags().BoolVar(&opts.deleteProtectedData, "delete-protected-data", false, "also delete configured backup roots")
	cmd.Flags().StringVar(&opts.confirmDeleteProtectedData, "confirm-delete-protected-data", "", "required value DELETE-PROTECTED-DATA when deleting configured backup roots")
	return cmd
}

func runReset(cfg config.Config, configPath string, opts resetOptions, out io.Writer) error {
	result, err := app.ResetLocalState(app.ResetRequest{
		Config:                     cfg,
		ConfigPath:                 configPath,
		Yes:                        opts.yes,
		DataDir:                    opts.dataDir,
		DeleteProtectedData:        opts.deleteProtectedData,
		ConfirmDeleteProtectedData: opts.confirmDeleteProtectedData,
	})
	if err != nil {
		return err
	}
	for _, path := range result.Removed {
		fmt.Fprintf(out, "removed: %s\n", path)
	}
	return nil
}
