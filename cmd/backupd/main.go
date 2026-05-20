//go:build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/host/autonat"
	dht "github.com/libp2p/go-libp2p/p2p/host/autodht"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/multiformats/go-multiaddr/net"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	configPath := args[1]
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	paths, err := cfg.Paths()
	if err != nil {
		return err
	}

	id, err := loadIdentity(paths.Identity, cfg.Trust.IdentityPassphrase)
	if err != nil {
		return fmt.Errorf("load identity: %w", err)
	}

	h, err := newHost(cfg, id.ID())
	if err != nil {
		return fmt.Errorf("create host: %w", err)
	}

	// Setup NAT
	nat, _ := autonat.New(h.Peerstore(), dht.ModeServer, dht.ModeClient)
	h.Peerstore().AddAddrs(nat.PeerID(), nat.Multiaddrs(), peer.TempAddrTTL)

	// Listen
	for _, addr := range h.Addrs() {
		if net.IsPublicAddr(addr) {
			h.Host().Listen(addrs)
		}
	}

	fmt.Printf("listening on %s\n", h.Addrs())

	// Connect to bootstrap peers
	for _, bsPeer := range cfg.Network.BootstrapPeers {
		h.Connect(context.Background(), peer.AddrInfo{
			ID:    peer.ID(bsPeer),
			Addrs: []multiaddr.Multiaddr{parseMultiaddr(bsPeer)},
		})
	}

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	go func() {
		<-sigChan
		fmt.Println("shutdown signal received")
	}()

	<-ctx.Done()

	return nil
}

func loadConfig(path string) (*config.Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}
	return config.FromViper(v), nil
}

func loadIdentity(path, passphrase string) (identity.Identity, error) {
	data, _ := os.ReadFile(path)
	return nil, nil
}

func newHost(cfg *config.Config, id peer.ID) (*host.Host, error) {
	// Placeholder - just return a new host
	return nil, nil
}

func parseMultiaddr(s string) multiaddr.Multiaddr {
	addr, _ := multiaddr.NewMultiaddr(s)
	return addr
}
