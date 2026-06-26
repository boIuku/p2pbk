package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"p2pbk/internal/app"
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
				if err := app.ApplyConfigSet(&cfg, opts.setKey, opts.setValue); err != nil {
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
				if err := app.SaveConfig(cfg, path); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "updated %s\n", path)
				return nil
			}

			summary, err := app.SummarizeConfig(cfg, configFileUsed())
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Current configuration:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Data dir: %s\n", summary.DataDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Store dir: %s\n", summary.StoreDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Cache dir: %s\n", summary.CacheDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Offer bytes: %d\n", summary.OfferBytes)
			fmt.Fprintf(cmd.OutOrStdout(), "Protocol: %s\n", summary.ProtocolID)
			fmt.Fprintf(cmd.OutOrStdout(), "Min peers: %d\n", summary.MinPeers)
			fmt.Fprintf(cmd.OutOrStdout(), "Local discovery: %t\n", summary.LocalDiscovery)
			fmt.Fprintf(cmd.OutOrStdout(), "Local discovery service: %s\n", summary.LocalDiscoveryService)
			fmt.Fprintf(cmd.OutOrStdout(), "Remote discovery: %t\n", summary.RemoteDiscovery)
			fmt.Fprintf(cmd.OutOrStdout(), "Remote discovery namespace: %s\n", summary.RemoteDiscoveryNamespace)
			fmt.Fprintf(cmd.OutOrStdout(), "Remote discovery interval: %s\n", summary.RemoteDiscoveryInterval)
			fmt.Fprintf(cmd.OutOrStdout(), "Public discovery peers: %d\n", len(summary.PublicDiscoveryPeers))
			fmt.Fprintf(cmd.OutOrStdout(), "Protected roots: %v\n", summary.ProtectedRoots)
			fmt.Fprintf(cmd.OutOrStdout(), "Max protected data: %d bytes\n", summary.MaxProtectedBytes)
			fmt.Fprintf(cmd.OutOrStdout(), "Schedule: %s\n", summary.Schedule)
			fmt.Fprintf(cmd.OutOrStdout(), "Retention: %s\n", summary.Retention)
			fmt.Fprintf(cmd.OutOrStdout(), "Data shards: %d\n", summary.DataShards)
			fmt.Fprintf(cmd.OutOrStdout(), "Parity shards: %d\n", summary.ParityShards)
			fmt.Fprintf(cmd.OutOrStdout(), "Challenge interval: %s\n", summary.ChallengeInterval)
			fmt.Fprintf(cmd.OutOrStdout(), "Reputation window: %s\n", summary.ReputationWindow)

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
	case "network.enable_remote_discovery":
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Network.EnableRemoteDiscovery = enabled
	case "network.remote_discovery_namespace":
		cfg.Network.RemoteDiscoveryNamespace = value
	case "network.remote_discovery_interval":
		cfg.Network.RemoteDiscoveryInterval = value
	case "network.public_discovery_peers":
		cfg.Network.PublicDiscoveryPeers = splitCSV(value)
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

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
