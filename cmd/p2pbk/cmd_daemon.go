package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"p2pbk/internal/config"
	p2pdht "p2pbk/internal/network/dht"
	p2pdiscovery "p2pbk/internal/network/discovery"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/peer/quota"
	"p2pbk/internal/peer/responder"
	"p2pbk/internal/trust/reputation"
)

func newDaemonCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Run the p2pbk background service",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := commandSignalContext()
			defer stop()

			cfg := currentConfig()
			return runDaemon(ctx, cfg, cmd.OutOrStdout())
		},
	}
	return cmd
}

func runDaemon(ctx context.Context, cfg config.Config, out interface{ Write([]byte) (int, error) }) error {
	paths, err := cfg.Paths()
	if err != nil {
		return err
	}

	host, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: cfg.Network.ListenAddrs})
	if err != nil {
		return err
	}
	defer host.Close()

	db, err := localdb.Open(paths.LocalDB)
	if err != nil {
		return err
	}
	defer db.Close()

	connected, err := p2pdiscovery.ConnectConfiguredAndSaved(ctx, host, cfg.Network.BootstrapPeers, db)
	if err != nil {
		return err
	}

	dht, err := p2pdht.New(ctx, host, p2pdht.ModeServer)
	if err != nil {
		return err
	}
	defer dht.Close()
	if err := dht.Bootstrap(ctx); err != nil {
		return err
	}

	q, err := quota.New(cfg.Storage.OfferBytes)
	if err != nil {
		return err
	}
	blobServer, err := blobserver.NewWithQuota(paths.StoreDir, q)
	if err != nil {
		return err
	}

	router := wire.NewRouter()
	blobServer.Register(router)
	reputationWindow, _ := time.ParseDuration(cfg.Trust.ReputationWindow)
	responder.New(blobServer, db, reputation.New(reputationWindow), responder.WithPeerID(host.ID())).Register(router)
	router.Register(host)

	fmt.Fprintf(out, "p2pbk peer: %s\n", host.ID())
	for _, addr := range host.Addrs() {
		fmt.Fprintf(out, "listening: %s/p2p/%s\n", addr, host.ID())
	}
	for _, peer := range connected {
		fmt.Fprintf(out, "connected bootstrap: %s\n", peer.ID)
	}
	if cfg.Network.EnableLocalDiscovery {
		localDiscovery, err := p2pdiscovery.StartLocal(ctx, host, db, cfg.Network.LocalDiscoveryService)
		if err != nil {
			fmt.Fprintf(out, "local discovery unavailable: %v\n", err)
		} else {
			defer localDiscovery.Close()
			fmt.Fprintf(out, "local discovery: %s\n", cfg.Network.LocalDiscoveryService)
		}
	}
	if err := p2pdiscovery.SaveConnectedPeers(db, host, "daemon"); err != nil {
		return err
	}
	fmt.Fprintf(out, "store: %s\n", filepath.Clean(paths.StoreDir))

	<-ctx.Done()
	return nil
}
