package main

import (
	"fmt"

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
			if configPath != "" {
				viper.SetConfigFile(configPath)
				if err := viper.ReadInConfig(); err != nil {
					return fmt.Errorf("read config: %w", err)
				}
				activeConfigPath = configPath
			}
			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&configPath, "config", "", "config file")
	_ = viper.BindPFlag("config", cmd.PersistentFlags().Lookup("config"))

	cmd.AddCommand(newInitCommand())
	cmd.AddCommand(newStatusCommand())
	cmd.AddCommand(newConfigCommand())
	cmd.AddCommand(newAddCommand())
	cmd.AddCommand(newBackupCommand())
	cmd.AddCommand(newRestoreCommand())
	cmd.AddCommand(newRepoCommand())
	cmd.AddCommand(newSnapshotCommand())

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
	return viper.ConfigFileUsed()
}
