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
	"p2pbk/internal/logging"
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
	logger := logging.New(out, "daemon")
	runtime, err := openDaemonRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer runtime.Close()

	connected, err := startDaemonNetworking(ctx, cfg, runtime)
	if err != nil {
		return err
	}
	if err := registerDaemonServices(cfg, runtime, logger.WithComponent("services")); err != nil {
		return err
	}
	discoveryServices, err := startDaemonDiscovery(ctx, cfg, runtime, logger.WithComponent("discovery"))
	if err != nil {
		return err
	}
	defer discoveryServices.Close()

	writeDaemonStartup(logger, runtime, connected)
	startDaemonJobs(ctx, cfg, runtime, logger)

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
		ListenAddrs:        cfg.Network.ListenAddrs,
		StaticRelays:       relayPeers,
		EnableNATPortMap:   true,
		EnableHolePunching: len(relayPeers) > 0,
		ForcePrivateRelay:  cfg.Network.ForcePrivateReachabilityForRelay,
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
	publicDiscoveryInfos, err := p2phost.ParseAddrInfos(cfg.Network.PublicDiscoveryPeers)
	if err != nil {
		return nil, fmt.Errorf("parse public discovery peer: %w", err)
	}
	infrastructureFilter := p2pdiscovery.NewPeerFilter(publicDiscoveryInfos)

	publicConnected, err := p2pdiscovery.ConnectConfiguredAndSaved(ctx, runtime.host, cfg.Network.PublicDiscoveryPeers, nil)
	if err != nil {
		return nil, err
	}
	connected, err := p2pdiscovery.ConnectConfiguredAndSaved(ctx, runtime.host, cfg.Network.BootstrapPeers, runtime.db, p2pdiscovery.WithIgnoredPeers(infrastructureFilter))
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

	return append(publicConnected, connected...), nil
}

func registerDaemonServices(cfg config.Config, runtime *daemonRuntime, logger *logging.Logger) error {
	q, err := quota.New(cfg.Storage.OfferBytes)
	if err != nil {
		return err
	}
	id, identityErr := loadDaemonIdentity(runtime.paths.Identity, cfg)
	if identityErr != nil {
		logger.Warn("identity_unavailable", "identity unavailable for signed receipts", "identity_unavailable", identityErr, nil)
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
	logger.Info("services_registered", "daemon services registered", logging.Field{
		"store_dir":          filepath.Clean(runtime.paths.StoreDir),
		"offer_bytes":        cfg.Storage.OfferBytes,
		"signed_receipts":    id != nil,
		"reputation_window":  cfg.Trust.ReputationWindow,
		"wire_protocol_id":   string(wire.ProtocolID),
		"local_peer_id":      runtime.host.ID().String(),
		"challenge_interval": cfg.Trust.ChallengeInterval,
	})
	return nil
}

func startDaemonDiscovery(ctx context.Context, cfg config.Config, runtime *daemonRuntime, logger *logging.Logger) (*daemonDiscoveryServices, error) {
	services := &daemonDiscoveryServices{}
	publicDiscoveryInfos, err := p2phost.ParseAddrInfos(cfg.Network.PublicDiscoveryPeers)
	if err != nil {
		return nil, fmt.Errorf("parse public discovery peer: %w", err)
	}
	infrastructureFilter := p2pdiscovery.NewPeerFilter(publicDiscoveryInfos)

	if err := p2pdiscovery.SaveConnectedPeers(runtime.db, runtime.host, "daemon", p2pdiscovery.WithIgnoredPeers(infrastructureFilter)); err != nil {
		return nil, err
	}
	switch cfg.Network.Discovery {
	case "remote":
		if runtime.dht == nil {
			return nil, errors.New("remote discovery requires dht")
		}
		interval, err := time.ParseDuration(cfg.Network.RemoteDiscoveryInterval)
		if err != nil {
			return nil, fmt.Errorf("parse network.remote_discovery_interval: %w", err)
		}
		remoteDiscovery, err := p2pdiscovery.StartRemote(ctx, runtime.host, runtime.db, runtime.dht, cfg.Network.RemoteDiscoveryNamespace,
			p2pdiscovery.WithRemoteInterval(interval),
			p2pdiscovery.WithRemoteIgnoredPeers(infrastructureFilter),
			p2pdiscovery.WithRemoteRequireRelayAddress(cfg.Network.ForcePrivateReachabilityForRelay),
			p2pdiscovery.WithRemoteObserver(func(event p2pdiscovery.Event) {
				writeDiscoveryEvent(logger, event)
			}),
		)
		if err != nil {
			logger.Error("remote_discovery_unavailable", "remote discovery unavailable", "remote_discovery_unavailable", err, logging.Field{"namespace": cfg.Network.RemoteDiscoveryNamespace})
		} else {
			services.remote = remoteDiscovery
			logger.Info("remote_discovery_started", "remote discovery started", logging.Field{
				"namespace":   cfg.Network.RemoteDiscoveryNamespace,
				"interval_ms": interval.Milliseconds(),
			})
		}
	case "local":
		localDiscovery, err := p2pdiscovery.StartLocalWithOptions(ctx, runtime.host, runtime.db, cfg.Network.LocalDiscoveryService, p2pdiscovery.WithObserver(func(event p2pdiscovery.Event) {
			writeDiscoveryEvent(logger, event)
		}))
		if err != nil {
			logger.Error("local_discovery_unavailable", "local discovery unavailable", "local_discovery_unavailable", err, logging.Field{"service": cfg.Network.LocalDiscoveryService})
		} else {
			services.local = localDiscovery
			logger.Info("local_discovery_started", "local discovery started", logging.Field{"service": cfg.Network.LocalDiscoveryService})
		}
	case "disabled":
		logger.Info("discovery_disabled", "peer discovery disabled", nil)
	}
	return services, nil
}

func (s *daemonDiscoveryServices) Close() error {
	if s == nil {
		return nil
	}
	return errors.Join(s.local.Close(), s.remote.Close())
}

func writeDiscoveryEvent(logger *logging.Logger, event p2pdiscovery.Event) {
	source := event.Source
	if source == "" {
		source = "unknown"
	}
	fields := logging.Field{"source": source, "addrs": event.Addrs}
	switch event.Type {
	case p2pdiscovery.EventFound:
		logger.Write(logging.Event{Level: "info", Event: "peer_discovered", Message: "discovery found peer", PeerID: event.PeerID, Fields: fields})
	case p2pdiscovery.EventConnected:
		logger.Write(logging.Event{Level: "info", Event: "peer_connected", Message: "discovery connected peer", PeerID: event.PeerID, Fields: fields})
	case p2pdiscovery.EventSaved:
		logger.Write(logging.Event{Level: "info", Event: "peer_saved", Message: "discovery saved peer", PeerID: event.PeerID, Fields: fields})
	case p2pdiscovery.EventConnectFailed:
		logger.Write(logging.Event{Level: "warn", Event: "peer_connect_failed", Message: "discovery connect failed", PeerID: event.PeerID, ErrorCode: "peer_connect_failed", Error: errorString(event.Err), Fields: fields})
	case p2pdiscovery.EventSaveFailed:
		logger.Write(logging.Event{Level: "warn", Event: "peer_save_failed", Message: "discovery save failed", PeerID: event.PeerID, ErrorCode: "peer_save_failed", Error: errorString(event.Err), Fields: fields})
	}
}

func writeDaemonStartup(logger *logging.Logger, runtime *daemonRuntime, connected []peer.AddrInfo) {
	addrs := make([]string, 0, len(runtime.host.Addrs()))
	for _, addr := range runtime.host.Addrs() {
		addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", addr, runtime.host.ID()))
	}
	connectedPeers := make([]string, 0, len(connected))
	for _, p := range connected {
		connectedPeers = append(connectedPeers, p.ID.String())
	}
	logger.Write(logging.Event{
		Level:   "info",
		Event:   "daemon_started",
		PeerID:  runtime.host.ID().String(),
		Message: "p2pbk daemon started",
		Fields: logging.Field{
			"listen_addrs":    addrs,
			"connected_peers": connectedPeers,
			"store_dir":       filepath.Clean(runtime.paths.StoreDir),
		},
	})
}

func startDaemonJobs(ctx context.Context, cfg config.Config, runtime *daemonRuntime, logger *logging.Logger) {
	scheduler.Start(ctx, scheduler.Options{
		Config:     cfg,
		ConfigPath: currentConfigPath(cfg),
		DB:         runtime.db,
		Logger:     logger.WithComponent("scheduler"),
	})
	if interval, err := time.ParseDuration(cfg.Trust.ChallengeInterval); err == nil && interval > 0 {
		audit.Start(ctx, audit.Options{Config: cfg, Host: runtime.host, DB: runtime.db, Logger: logger.WithComponent("audit")}, interval)
		logger.Info("audit_scheduler_started", "audit scheduler started", logging.Field{"interval_ms": interval.Milliseconds()})
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
