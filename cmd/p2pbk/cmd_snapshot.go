package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newSnapshotCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "List and inspect Kopia snapshots",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List snapshots",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := openExistingKopiaEngine()
			if err != nil {
				return friendlyRepositoryError(err)
			}
			snapshots, err := engine.ListSnapshots(context.Background())
			if err != nil {
				return friendlyRepositoryError(err)
			}
			for _, s := range snapshots {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", s.ID, s.StartTime, s.Source, s.Description)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "show <snapshot-id|latest>",
		Short: "Show snapshot details",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: p2pbk snapshot show <snapshot-id|latest>")
			}
			engine, err := openExistingKopiaEngine()
			if err != nil {
				return friendlyRepositoryError(err)
			}
			s, err := engine.ShowSnapshot(context.Background(), args[0])
			if err != nil {
				return friendlyRepositoryError(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id: %s\n", s.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "source: %s\n", s.Source)
			fmt.Fprintf(cmd.OutOrStdout(), "description: %s\n", s.Description)
			fmt.Fprintf(cmd.OutOrStdout(), "start: %s\n", s.StartTime)
			fmt.Fprintf(cmd.OutOrStdout(), "end: %s\n", s.EndTime)
			fmt.Fprintf(cmd.OutOrStdout(), "root: %s\n", s.Root)
			fmt.Fprintf(cmd.OutOrStdout(), "files: %d\n", s.Stats.TotalFileCount)
			fmt.Fprintf(cmd.OutOrStdout(), "dirs: %d\n", s.Stats.TotalDirectoryCount)
			return nil
		},
	})

	return cmd
}
