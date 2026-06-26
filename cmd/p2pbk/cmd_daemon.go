package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spf13/cobra"

	"p2pbk/internal/audit"
	"p2pbk/internal/config"
	p2pdht "p2pbk/internal/network/dht"
	p2pdiscovery "p2pbk/internal/network/discovery"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/peer/quota"
	"p2pbk/internal/peer/responder"
	"p2pbk/internal/scheduler"
	"p2pbk/internal/trust/identity"
	"p2pbk/internal/trust/reputation"
)

type daemonRuntime struct {
	paths *config.Paths
	host  corehost.Host
	db    *localdb.DB
	dht   *p2pdht.DHT
}

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
	runtime, err := openDaemonRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer runtime.Close()

	connected, err := startDaemonNetworking(ctx, cfg, runtime)
	if err != nil {
		return err
	}
	if err := registerDaemonServices(cfg, runtime, out); err != nil {
		return err
	}
	localDiscovery, err := startDaemonDiscovery(ctx, cfg, runtime, out)
	if err != nil {
		return err
	}
	defer localDiscovery.Close()

	writeDaemonStartup(out, runtime, connected)
	startDaemonJobs(ctx, cfg, runtime, out)

	<-ctx.Done()
	return nil
}

func openDaemonRuntime(ctx context.Context, cfg config.Config) (*daemonRuntime, error) {
	paths, err := cfg.Paths()
	if err != nil {
		return nil, err
	}

	host, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: cfg.Network.ListenAddrs})
	if err != nil {
		return nil, err
	}

	db, err := localdb.Open(paths.LocalDB)
	if err != nil {
		_ = host.Close()
		return nil, err
	}

	return &daemonRuntime{paths: paths, host: host, db: db}, nil
}

func (r *daemonRuntime) Close() error {
	var out error
	if r.dht != nil {
		out = errors.Join(out, r.dht.Close())
	}
	if r.db != nil {
		out = errors.Join(out, r.db.Close())
	}
	if r.host != nil {
		out = errors.Join(out, r.host.Close())
	}
	return out
}

func startDaemonNetworking(ctx context.Context, cfg config.Config, runtime *daemonRuntime) ([]peer.AddrInfo, error) {
	connected, err := p2pdiscovery.ConnectConfiguredAndSaved(ctx, runtime.host, cfg.Network.BootstrapPeers, runtime.db)
	if err != nil {
		return nil, err
	}

	dht, err := p2pdht.New(ctx, runtime.host, p2pdht.ModeServer)
	if err != nil {
		return nil, err
	}
	if err := dht.Bootstrap(ctx); err != nil {
		_ = dht.Close()
		return nil, err
	}
	runtime.dht = dht

	return connected, nil
}

func registerDaemonServices(cfg config.Config, runtime *daemonRuntime, out interface{ Write([]byte) (int, error) }) error {
	q, err := quota.New(cfg.Storage.OfferBytes)
	if err != nil {
		return err
	}
	id, identityErr := loadDaemonIdentity(runtime.paths.Identity, cfg)
	if identityErr != nil {
		fmt.Fprintf(out, "identity unavailable for signed receipts: %v\n", identityErr)
	}
	blobServer, err := blobserver.NewWithOptions(runtime.paths.StoreDir, blobserver.Options{
		Quota:    q,
		Identity: id,
		PeerID:   runtime.host.ID().String(),
	})
	if err != nil {
		return err
	}

	router := wire.NewRouter()
	blobServer.Register(router)
	reputationWindow, _ := time.ParseDuration(cfg.Trust.ReputationWindow)
	responder.New(blobServer, runtime.db, reputation.New(reputationWindow), responder.WithPeerID(runtime.host.ID())).Register(router)
	router.Register(runtime.host)
	return nil
}

func startDaemonDiscovery(ctx context.Context, cfg config.Config, runtime *daemonRuntime, out interface{ Write([]byte) (int, error) }) (*p2pdiscovery.LocalService, error) {
	if err := p2pdiscovery.SaveConnectedPeers(runtime.db, runtime.host, "daemon"); err != nil {
		return nil, err
	}
	if !cfg.Network.EnableLocalDiscovery {
		return nil, nil
	}
	localDiscovery, err := p2pdiscovery.StartLocalWithOptions(ctx, runtime.host, runtime.db, cfg.Network.LocalDiscoveryService, p2pdiscovery.WithObserver(func(event p2pdiscovery.Event) {
		writeDiscoveryEvent(out, event)
	}))
	if err != nil {
		fmt.Fprintf(out, "local discovery unavailable: %v\n", err)
		return nil, nil
	}
	fmt.Fprintf(out, "local discovery: %s\n", cfg.Network.LocalDiscoveryService)
	return localDiscovery, nil
}

func writeDiscoveryEvent(out interface{ Write([]byte) (int, error) }, event p2pdiscovery.Event) {
	source := event.Source
	if source == "" {
		source = "unknown"
	}
	switch event.Type {
	case p2pdiscovery.EventFound:
		fmt.Fprintf(out, "local discovery found (%s): %s\n", source, event.PeerID)
	case p2pdiscovery.EventConnected:
		fmt.Fprintf(out, "local discovery connected (%s): %s\n", source, event.PeerID)
	case p2pdiscovery.EventSaved:
		fmt.Fprintf(out, "local discovery saved (%s): %s\n", source, event.PeerID)
	case p2pdiscovery.EventConnectFailed:
		fmt.Fprintf(out, "local discovery connect failed (%s): %s: %v\n", source, event.PeerID, event.Err)
	case p2pdiscovery.EventSaveFailed:
		fmt.Fprintf(out, "local discovery save failed (%s): %s: %v\n", source, event.PeerID, event.Err)
	}
}

func writeDaemonStartup(out interface{ Write([]byte) (int, error) }, runtime *daemonRuntime, connected []peer.AddrInfo) {
	fmt.Fprintf(out, "p2pbk peer: %s\n", runtime.host.ID())
	for _, addr := range runtime.host.Addrs() {
		fmt.Fprintf(out, "listening: %s/p2p/%s\n", addr, runtime.host.ID())
	}
	for _, peer := range connected {
		fmt.Fprintf(out, "connected peer: %s\n", peer.ID)
	}
	fmt.Fprintf(out, "store: %s\n", filepath.Clean(runtime.paths.StoreDir))
}

func startDaemonJobs(ctx context.Context, cfg config.Config, runtime *daemonRuntime, out interface{ Write([]byte) (int, error) }) {
	scheduler.Start(ctx, scheduler.Options{
		Config:     cfg,
		ConfigPath: currentConfigPath(cfg),
		DB:         runtime.db,
		Output:     out,
	})
	if interval, err := time.ParseDuration(cfg.Trust.ChallengeInterval); err == nil && interval > 0 {
		audit.Start(ctx, audit.Options{Config: cfg, Host: runtime.host, DB: runtime.db, Output: out}, interval)
		fmt.Fprintf(out, "audits: every %s\n", interval)
	}
}

func loadDaemonIdentity(path string, cfg config.Config) (*identity.Identity, error) {
	passphrase := cfg.Trust.IdentityPassphrase
	if passphrase == "" {
		passphrase = os.Getenv("P2PBK_IDENTITY_PASSPHRASE")
	}
	if passphrase == "" {
		return nil, fmt.Errorf("identity passphrase is not set")
	}
	return identity.Load(path, passphrase)
}
