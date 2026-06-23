package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"p2pbk/internal/config"
)

type configOptions struct {
	setKey   string
	setValue string
	getKey   string
}

func newConfigCommand() *cobra.Command {
	opts := configOptions{}

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or update p2pbk configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			v := viper.GetViper()

			if opts.getKey != "" {
				val := v.Get(opts.getKey)
				if val == nil {
					return fmt.Errorf("setting %q not found", opts.getKey)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%v\n", val)
				return nil
			}

			cfg := currentConfig()
			if opts.setKey != "" {
				if opts.setValue == "" {
					return fmt.Errorf("--value is required with --set")
				}
				if err := applyConfigSet(&cfg, opts.setKey, opts.setValue); err != nil {
					return err
				}
				path := viper.ConfigFileUsed()
				if path == "" {
					paths, err := cfg.Paths()
					if err != nil {
						return err
					}
					path = paths.DataDir + "/config.json"
				}
				if err := saveConfigFile(cfg, path); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "updated %s\n", path)
				return nil
			}

			paths, err := cfg.Paths()
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Current configuration:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Data dir: %s\n", paths.DataDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Store dir: %s\n", paths.StoreDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Cache dir: %s\n", paths.CacheDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Cache size: %d bytes\n", cfg.Storage.CacheSizeBytes)
			fmt.Fprintf(cmd.OutOrStdout(), "Offer bytes: %d\n", cfg.Storage.OfferBytes)
			fmt.Fprintf(cmd.OutOrStdout(), "Protocol: %s\n", cfg.Network.ProtocolID)
			fmt.Fprintf(cmd.OutOrStdout(), "Min peers: %d\n", cfg.Network.MinPeers)
			fmt.Fprintf(cmd.OutOrStdout(), "Local discovery: %t\n", cfg.Network.EnableLocalDiscovery)
			fmt.Fprintf(cmd.OutOrStdout(), "Local discovery service: %s\n", cfg.Network.LocalDiscoveryService)
			fmt.Fprintf(cmd.OutOrStdout(), "Protected roots: %v\n", cfg.Backup.Roots)
			fmt.Fprintf(cmd.OutOrStdout(), "Max protected data: %d bytes\n", cfg.Backup.MaxBytes)
			fmt.Fprintf(cmd.OutOrStdout(), "Schedule: %s\n", cfg.Backup.Schedule)
			fmt.Fprintf(cmd.OutOrStdout(), "Retention: %s\n", cfg.Backup.Retention)
			fmt.Fprintf(cmd.OutOrStdout(), "Data shards: %d\n", cfg.Backup.DataShards)
			fmt.Fprintf(cmd.OutOrStdout(), "Parity shards: %d\n", cfg.Backup.ParityShards)
			fmt.Fprintf(cmd.OutOrStdout(), "Challenge interval: %s\n", cfg.Trust.ChallengeInterval)
			fmt.Fprintf(cmd.OutOrStdout(), "Reputation window: %s\n", cfg.Trust.ReputationWindow)

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.setKey, "set", "", "set a config value (key)")
	cmd.Flags().StringVar(&opts.setValue, "value", "", "value to set")
	cmd.Flags().StringVar(&opts.getKey, "get", "", "get a config value")

	return cmd
}

func saveConfigFile(cfg config.Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return nil
}

func loadConfigFile(path string) (config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config.Config{}, err
	}

	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config.Config{}, err
	}

	// Apply defaults for missing values
	cfg.Node.DataDir = os.ExpandEnv(cfg.Node.DataDir)
	return cfg, nil
}

func applyConfigSet(cfg *config.Config, key, value string) error {
	switch key {
	case "node.data_dir":
		cfg.Node.DataDir = value
	case "node.display_name":
		cfg.Node.DisplayName = value
	case "storage.store_dir":
		cfg.Storage.StoreDir = value
	case "storage.cache_dir":
		cfg.Storage.CacheDir = value
	case "storage.offer_bytes":
		n, err := parseByteSize(value)
		if err != nil {
			return err
		}
		cfg.Storage.OfferBytes = n
	case "network.protocol_id":
		cfg.Network.ProtocolID = value
	case "network.local_discovery_service":
		cfg.Network.LocalDiscoveryService = value
	case "backup.max_bytes":
		n, err := parseByteSize(value)
		if err != nil {
			return err
		}
		cfg.Backup.MaxBytes = n
	case "backup.schedule":
		cfg.Backup.Schedule = value
	case "backup.retention":
		cfg.Backup.Retention = value
	case "trust.challenge_interval":
		cfg.Trust.ChallengeInterval = value
	case "trust.reputation_window":
		cfg.Trust.ReputationWindow = value
	default:
		return fmt.Errorf("unsupported config key %q", key)
	}
	return cfg.Validate()
}
