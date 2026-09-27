package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"p2pbk/internal/app"
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
				if opts.getKey == "trust.identity_passphrase" || strings.Contains(strings.ToLower(opts.getKey), "password") || strings.Contains(strings.ToLower(opts.getKey), "secret") {
					fmt.Fprintln(cmd.OutOrStdout(), "<redacted>")
					return nil
				}
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
			fmt.Fprintf(cmd.OutOrStdout(), "Per-owner offer bytes: %d\n", summary.PerOwnerOfferBytes)
			fmt.Fprintf(cmd.OutOrStdout(), "Protocol: %s\n", summary.ProtocolID)
			fmt.Fprintf(cmd.OutOrStdout(), "Min peers: %d\n", summary.MinPeers)
			fmt.Fprintf(cmd.OutOrStdout(), "Discovery: %s\n", summary.Discovery)
			fmt.Fprintf(cmd.OutOrStdout(), "Local discovery service: %s\n", summary.LocalDiscoveryService)
			fmt.Fprintf(cmd.OutOrStdout(), "Remote discovery namespace: %s\n", summary.RemoteDiscoveryNamespace)
			fmt.Fprintf(cmd.OutOrStdout(), "Remote discovery interval: %s\n", summary.RemoteDiscoveryInterval)
			fmt.Fprintf(cmd.OutOrStdout(), "Public discovery peers: %d\n", len(summary.PublicDiscoveryPeers))
			fmt.Fprintf(cmd.OutOrStdout(), "Protected roots: %v\n", summary.ProtectedRoots)
			fmt.Fprintf(cmd.OutOrStdout(), "Max protected data: %d bytes\n", summary.MaxProtectedBytes)
			fmt.Fprintf(cmd.OutOrStdout(), "Schedule: %s\n", summary.Schedule)
			fmt.Fprintf(cmd.OutOrStdout(), "Retention: %s\n", summary.Retention)
			fmt.Fprintf(cmd.OutOrStdout(), "Recovery external path: %s\n", summary.RecoveryExternalPath)
			fmt.Fprintf(cmd.OutOrStdout(), "Recovery generations: %d\n", summary.RecoveryRetainGenerations)
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
