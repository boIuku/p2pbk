package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"p2pbk/internal/app"
	"p2pbk/internal/kopiaengine"
	"p2pbk/internal/secrets"
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
			cfg := currentConfig()
			path, err := app.EnsureRepository(context.Background(), cfg, currentConfigPath(cfg), secrets.FromEnv())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "repository ready: %s\n", path)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "connect",
		Short: "Connect local Kopia config to the p2pbk repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := currentConfig()
			path, err := app.EnsureRepository(context.Background(), cfg, currentConfigPath(cfg), secrets.FromEnv())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "repository connected: %s\n", path)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show Kopia repository status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := currentConfig()
			status, err := app.GetRepositoryStatus(context.Background(), cfg, currentConfigPath(cfg), secrets.FromEnv())
			if err != nil {
				return friendlyRepositoryError(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "config: %s\n", status.ConfigPath)
			fmt.Fprintf(cmd.OutOrStdout(), "storage: %s\n", status.StorageType)
			fmt.Fprintf(cmd.OutOrStdout(), "snapshots: %d\n", status.SnapshotCount)
			if status.UniqueID != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "unique_id: %s\n", status.UniqueID)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "maintenance",
		Short: "Show Kopia maintenance schedule",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := currentConfig()
			status, err := app.GetMaintenanceStatus(context.Background(), cfg, currentConfigPath(cfg), secrets.FromEnv())
			if err != nil {
				return friendlyRepositoryError(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "next_full: %s\n", status.NextFull.Format("2006-01-02T15:04:05Z07:00"))
			fmt.Fprintf(cmd.OutOrStdout(), "next_quick: %s\n", status.NextQuick.Format("2006-01-02T15:04:05Z07:00"))
			return nil
		},
	})

	maintenanceRun := &cobra.Command{
		Use:   "run",
		Short: "Apply retention and run repository maintenance",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := currentConfig()
			result, err := app.RunMaintenance(context.Background(), cfg, currentConfigPath(cfg), secrets.FromEnv())
			if err != nil {
				return friendlyRepositoryError(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "maintenance complete; deleted_snapshots: %d\n", result.DeletedSnapshots)
			return nil
		},
	}
	for _, child := range cmd.Commands() {
		if child.Use == "maintenance" {
			child.AddCommand(maintenanceRun)
			break
		}
	}

	return cmd
}

func friendlyRepositoryError(err error) error {
	if errors.Is(err, kopiaengine.ErrRepositoryNotConnected) {
		return fmt.Errorf("repository is not initialized yet; run `p2pbk repo init` or `p2pbk backup now` first")
	}
	return err
}
