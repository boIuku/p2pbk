package main

import (
	"context"
	"errors"
	"fmt"
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
	"p2pbk/internal/secrets"
	"p2pbk/internal/trust/identity"
	"p2pbk/internal/trust/reputation"
)

type daemonRuntime struct {
	paths *config.Paths
	host  corehost.Host
	db    *localdb.DB
	dht   *p2pdht.DHT
}

type daemonDiscoveryServices struct {
	local  *p2pdiscovery.LocalService
	remote *p2pdiscovery.RemoteService
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
	discoveryServices, err := startDaemonDiscovery(ctx, cfg, runtime, out)
	if err != nil {
		return err
	}
	defer discoveryServices.Close()

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

	relayPeers, err := p2phost.ParseAddrInfos(cfg.Network.PublicDiscoveryPeers)
	if err != nil {
		return nil, fmt.Errorf("parse public discovery peer: %w", err)
	}
	host, err := p2phost.New(ctx, p2phost.Options{
		ListenAddrs:       cfg.Network.ListenAddrs,
		StaticRelays:      relayPeers,
		EnableNATPortMap:  true,
		EnableHolePunching: len(relayPeers) > 0,
		ForcePrivateRelay: cfg.Network.ForcePrivateReachabilityForRelay,
	})
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
	bootstrapPeers := append([]string(nil), cfg.Network.BootstrapPeers...)
	bootstrapPeers = append(bootstrapPeers, cfg.Network.PublicDiscoveryPeers...)
	connected, err := p2pdiscovery.ConnectConfiguredAndSaved(ctx, runtime.host, bootstrapPeers, runtime.db)
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

func startDaemonDiscovery(ctx context.Context, cfg config.Config, runtime *daemonRuntime, out interface{ Write([]byte) (int, error) }) (*daemonDiscoveryServices, error) {
	services := &daemonDiscoveryServices{}
	if err := p2pdiscovery.SaveConnectedPeers(runtime.db, runtime.host, "daemon"); err != nil {
		return nil, err
	}
	if cfg.Network.EnableRemoteDiscovery {
		if runtime.dht == nil {
			return nil, errors.New("remote discovery requires dht")
		}
		interval, err := time.ParseDuration(cfg.Network.RemoteDiscoveryInterval)
		if err != nil {
			return nil, fmt.Errorf("parse network.remote_discovery_interval: %w", err)
		}
		remoteDiscovery, err := p2pdiscovery.StartRemote(ctx, runtime.host, runtime.db, runtime.dht, cfg.Network.RemoteDiscoveryNamespace,
			p2pdiscovery.WithRemoteInterval(interval),
			p2pdiscovery.WithRemoteObserver(func(event p2pdiscovery.Event) {
				writeDiscoveryEvent(out, event)
			}),
		)
		if err != nil {
			fmt.Fprintf(out, "remote discovery unavailable: %v\n", err)
		} else {
			services.remote = remoteDiscovery
			fmt.Fprintf(out, "remote discovery: %s every %s\n", cfg.Network.RemoteDiscoveryNamespace, interval)
		}
	}
	if cfg.Network.EnableLocalDiscovery {
		localDiscovery, err := p2pdiscovery.StartLocalWithOptions(ctx, runtime.host, runtime.db, cfg.Network.LocalDiscoveryService, p2pdiscovery.WithObserver(func(event p2pdiscovery.Event) {
			writeDiscoveryEvent(out, event)
		}))
		if err != nil {
			fmt.Fprintf(out, "local discovery unavailable: %v\n", err)
		} else {
			services.local = localDiscovery
			fmt.Fprintf(out, "local discovery: %s\n", cfg.Network.LocalDiscoveryService)
		}
	}
	return services, nil
}

func (s *daemonDiscoveryServices) Close() error {
	if s == nil {
		return nil
	}
	return errors.Join(s.local.Close(), s.remote.Close())
}

func writeDiscoveryEvent(out interface{ Write([]byte) (int, error) }, event p2pdiscovery.Event) {
	source := event.Source
	if source == "" {
		source = "unknown"
	}
	switch event.Type {
	case p2pdiscovery.EventFound:
		fmt.Fprintf(out, "discovery found (%s): %s\n", source, event.PeerID)
	case p2pdiscovery.EventConnected:
		fmt.Fprintf(out, "discovery connected (%s): %s\n", source, event.PeerID)
	case p2pdiscovery.EventSaved:
		fmt.Fprintf(out, "discovery saved (%s): %s\n", source, event.PeerID)
	case p2pdiscovery.EventConnectFailed:
		fmt.Fprintf(out, "discovery connect failed (%s): %s: %v\n", source, event.PeerID, event.Err)
	case p2pdiscovery.EventSaveFailed:
		fmt.Fprintf(out, "discovery save failed (%s): %s: %v\n", source, event.PeerID, event.Err)
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
		passphrase = secrets.FromEnv()
	}
	if passphrase == "" {
		return nil, fmt.Errorf("password is not set")
	}
	return identity.Load(path, passphrase)
}
