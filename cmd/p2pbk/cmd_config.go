package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
			viper := viper.GetViper()

			if opts.getKey != "" {
				val := viper.Get(opts.getKey)
				if val == nil {
					return fmt.Errorf("setting %q not found", opts.getKey)
				}
				fmt.Printf("%v\n", val)
				return nil
			}

			cfg := config.FromViper(viper)
			paths, _ := cfg.Paths()

			fmt.Printf("Current configuration:\n")
			fmt.Printf("Data dir: %s\n", paths.DataDir)
			fmt.Printf("Store dir: %s\n", paths.StoreDir)
			fmt.Printf("Cache dir: %s\n", paths.CacheDir)
			fmt.Printf("Cache size: %d bytes\n", cfg.Storage.CacheSizeBytes)
			fmt.Printf("Offer bytes: %d\n", cfg.Storage.OfferBytes)
			fmt.Printf("Protocol: %s\n", cfg.Network.ProtocolID)
			fmt.Printf("Min peers: %d\n", cfg.Network.MinPeers)
			fmt.Printf("Data shards: %d\n", cfg.Backup.DataShards)
			fmt.Printf("Parity shards: %d\n", cfg.Backup.ParityShards)
			fmt.Printf("Challenge interval: %s\n", cfg.Trust.ChallengeInterval)
			fmt.Printf("Reputation window: %s\n", cfg.Trust.ReputationWindow)
			fmt.Println()

			// Save config
			if err := saveConfigFile(cfg, paths.DataDir+"/config.yaml"); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.setKey, "set", "", "set a config value (key)")
	cmd.Flags().StringVar(&opts.setValue, "value", "", "value to set")
	cmd.Flags().StringVar(&opts.getKey, "get", "", "get a config value")

	// Wire set flag
	if opts.setKey != "" && opts.setValue != "" {
		key := strings.ReplaceAll(opts.setKey, ".", "_")
		viper.Set(key, opts.setValue)
	}

	return cmd
}

func saveConfigFile(cfg config.Config, path string) error {
	data, _ := json.MarshalIndent(cfg, "", "  ")
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