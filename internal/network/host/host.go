package host

import (
	"context"
	"fmt"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Options struct {
	ListenAddrs []string
	PrivateKey  crypto.PrivKey
}

func New(ctx context.Context, opts Options) (corehost.Host, error) {
	listenAddrs := opts.ListenAddrs
	if len(listenAddrs) == 0 {
		listenAddrs = []string{"/ip4/127.0.0.1/tcp/0"}
	}

	libp2pOpts := []libp2p.Option{
		libp2p.ListenAddrStrings(listenAddrs...),
	}
	if opts.PrivateKey != nil {
		libp2pOpts = append(libp2pOpts, libp2p.Identity(opts.PrivateKey))
	}

	h, err := libp2p.New(libp2pOpts...)
	if err != nil {
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = h.Close()
	}()

	return h, nil
}

func AddrInfo(h corehost.Host) peer.AddrInfo {
	return peer.AddrInfo{
		ID:    h.ID(),
		Addrs: h.Addrs(),
	}
}

func Connect(ctx context.Context, h corehost.Host, info peer.AddrInfo) error {
	if err := h.Connect(ctx, info); err != nil {
		return fmt.Errorf("connect to peer %s: %w", info.ID, err)
	}
	return nil
}
