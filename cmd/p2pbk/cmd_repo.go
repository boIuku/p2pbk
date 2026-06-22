package main

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/spf13/cobra"
)

func newRepoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage the Kopia repository backed by p2pbk storage",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Initialize and connect the Kopia repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := openKopiaEngine()
			if err != nil {
				return err
			}
			if err := engine.EnsureRepository(context.Background()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "repository ready: %s\n", engine.RepositoryConfigPath())
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "connect",
		Short: "Connect local Kopia config to the p2pbk repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := openKopiaEngine()
			if err != nil {
				return err
			}
			if err := engine.EnsureRepository(context.Background()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "repository connected: %s\n", engine.RepositoryConfigPath())
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show Kopia repository status",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := openKopiaEngine()
			if err != nil {
				return err
			}
			status, err := engine.Status(context.Background())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "config: %s\n", status.ConfigPath)
			fmt.Fprintf(cmd.OutOrStdout(), "storage: %s\n", status.StorageType)
			fmt.Fprintf(cmd.OutOrStdout(), "snapshots: %d\n", status.SnapshotCount)
			if len(status.UniqueID) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "unique_id: %s\n", hex.EncodeToString(status.UniqueID))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "maintenance",
		Short: "Show Kopia maintenance schedule",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := openKopiaEngine()
			if err != nil {
				return err
			}
			status, err := engine.MaintenanceStatus(context.Background())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "next_full: %s\n", status.NextFull.Format("2006-01-02T15:04:05Z07:00"))
			fmt.Fprintf(cmd.OutOrStdout(), "next_quick: %s\n", status.NextQuick.Format("2006-01-02T15:04:05Z07:00"))
			return nil
		},
	})

	return cmd
}
