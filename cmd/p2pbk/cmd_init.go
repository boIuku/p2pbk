package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"p2pbk/internal/app"
	"p2pbk/internal/config"
	"p2pbk/internal/secrets"
)

type initOptions struct {
	dataDir              string
	backupDir            string
	maxSpace             string
	offerSpace           string
	schedule             string
	retention            string
	configPath           string
	passphrase           string
	start                bool
	displayName          string
	bootstrapPeers       []string
	publicDiscoveryPeers []string
}

func newInitCommand() *cobra.Command {
	opts := initOptions{
		dataDir:    config.Default().Node.DataDir,
		maxSpace:   "10GB",
		offerSpace: "10GB",
		schedule:   "daily",
		retention:  "30d",
		start:      true,
	}

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize p2pbk and start background backup service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.passphrase == "" {
				opts.passphrase = secrets.FromEnv()
			}
			if opts.configPath == "" {
				opts.configPath = configFileUsed()
			}
			return runInit(opts, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&opts.dataDir, "data-dir", opts.dataDir, "node data directory")
	cmd.Flags().StringVar(&opts.backupDir, "dir", "", "directory to protect")
	cmd.Flags().StringVar(&opts.maxSpace, "max-space", opts.maxSpace, "maximum source data to protect, such as 10GB")
	cmd.Flags().StringVar(&opts.offerSpace, "offer-space", opts.offerSpace, "maximum local storage to offer peers, such as 20GB")
	cmd.Flags().StringVar(&opts.schedule, "schedule", opts.schedule, "backup policy schedule")
	cmd.Flags().StringVar(&opts.retention, "retention", opts.retention, "snapshot retention policy")
	cmd.Flags().StringVar(&opts.displayName, "name", "", "human-readable node name")
	cmd.Flags().StringSliceVar(&opts.bootstrapPeers, "bootstrap-peer", nil, "bootstrap peer multiaddr; repeat for multiple peers")
	cmd.Flags().StringSliceVar(&opts.publicDiscoveryPeers, "public-discovery-peer", nil, "public discovery peer multiaddr; repeat for multiple peers")
	cmd.Flags().StringVar(&opts.passphrase, "passphrase", "", "identity encryption passphrase")
	cmd.Flags().BoolVar(&opts.start, "start", opts.start, "start p2pbk in the background after initialization")

	return cmd
}

func runInit(opts initOptions, out io.Writer) error {
	result, err := app.Initialize(app.InitRequest{
		DataDir:              opts.dataDir,
		BackupDir:            opts.backupDir,
		MaxSpace:             opts.maxSpace,
		OfferSpace:           opts.offerSpace,
		Schedule:             opts.schedule,
		Retention:            opts.retention,
		ConfigPath:           opts.configPath,
		Passphrase:           opts.passphrase,
		Start:                opts.start,
		DisplayName:          opts.displayName,
		BootstrapPeers:       opts.bootstrapPeers,
		PublicDiscoveryPeers: opts.publicDiscoveryPeers,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "initialized p2pbk node at %s\n", result.DataDir)
	fmt.Fprintf(out, "config: %s\n", result.ConfigPath)
	fmt.Fprintf(out, "identity fingerprint: %s\n", result.IdentityFingerprint)
	fmt.Fprintf(out, "protecting up to %s from %d director%s\n", app.FormatBytes(result.MaxProtectedBytes), result.ProtectedRoots, plural(result.ProtectedRoots))
	fmt.Fprintf(out, "network storage required for that policy: about %s\n", app.FormatBytes(result.NetworkStorageBytes))
	fmt.Fprintf(out, "offering up to %s to other peers\n", app.FormatBytes(result.OfferBytes))
	if result.Started {
		fmt.Fprintln(out, "p2pbk is running in the background")
	}
	return nil
}

func plural(count int) string {
	if count == 1 {
		return "y"
	}
	return "ies"
}
