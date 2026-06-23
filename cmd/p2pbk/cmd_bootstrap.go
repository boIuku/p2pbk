package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"p2pbk/internal/config"
	p2pdht "p2pbk/internal/network/dht"
	p2phost "p2pbk/internal/network/host"
)

func newBootstrapCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "bootstrap",
		Short:  "Run a bootstrap/discovery node",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := commandSignalContext()
			defer stop()

			cfg := currentConfig()
			return runBootstrap(ctx, cfg, cmd.OutOrStdout())
		},
	}
	return cmd
}

func runBootstrap(ctx context.Context, cfg config.Config, out interface{ Write([]byte) (int, error) }) error {
	host, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: cfg.Network.ListenAddrs})
	if err != nil {
		return err
	}
	defer host.Close()

	dht, err := p2pdht.New(ctx, host, p2pdht.ModeServer)
	if err != nil {
		return err
	}
	defer dht.Close()
	if err := dht.Bootstrap(ctx); err != nil {
		return err
	}

	fmt.Fprintf(out, "p2pbk bootstrap peer: %s\n", host.ID())
	for _, addr := range host.Addrs() {
		fmt.Fprintf(out, "%s/p2p/%s\n", addr, host.ID())
	}

	<-ctx.Done()
	return nil
}
