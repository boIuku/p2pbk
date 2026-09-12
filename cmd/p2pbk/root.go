package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"p2pbk/internal/config"
	"p2pbk/internal/version"
)

var activeConfigPath string

func newRootCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "p2pbk",
		Short: "Peer-to-peer backup CLI",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.CommandPath() == "p2pbk recovery import" {
				return nil
			}
			path := configPath
			if path == "" {
				path = defaultConfigPath()
			}
			if path == "" {
				return nil
			}
			viper.SetConfigFile(path)
			activeConfigPath = path
			if cmd.CommandPath() == "p2pbk init" {
				if _, err := os.Stat(path); os.IsNotExist(err) {
					return nil
				}
			}
			if err := viper.ReadInConfig(); err != nil {
				return fmt.Errorf("read config: %w", err)
			}
			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&configPath, "config", "", "config file")
	_ = viper.BindPFlag("config", cmd.PersistentFlags().Lookup("config"))

	cmd.AddCommand(newInitCommand())
	cmd.AddCommand(newStatusCommand())
	cmd.AddCommand(newTUICommand())
	cmd.AddCommand(newConfigCommand())
	cmd.AddCommand(newAddCommand())
	cmd.AddCommand(newBackupCommand())
	cmd.AddCommand(newRestoreCommand())
	cmd.AddCommand(newRepoCommand())
	cmd.AddCommand(newSnapshotCommand())
	cmd.AddCommand(newPeersCommand())
	cmd.AddCommand(newAuditCommand())
	cmd.AddCommand(newResetCommand())
	cmd.AddCommand(newDaemonCommand())
	cmd.AddCommand(newBootstrapCommand())
	cmd.AddCommand(newRecoveryCommand())

	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print build version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version.String())
		},
	})

	return cmd
}

func currentConfig() config.Config {
	if configFileUsed() != "" {
		cfg, err := config.Load(configFileUsed())
		if err == nil {
			return cfg
		}
	}
	return config.Default()
}

func configFileUsed() string {
	if activeConfigPath != "" {
		return activeConfigPath
	}
	if used := viper.ConfigFileUsed(); used != "" {
		return used
	}
	return defaultConfigPath()
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".p2pbk", "config.json")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}
