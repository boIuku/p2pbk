package host

import (
	"context"
	"fmt"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
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

func ParseAddrInfo(addr string) (peer.AddrInfo, error) {
	maddr, err := multiaddr.NewMultiaddr(addr)
	if err != nil {
		return peer.AddrInfo{}, fmt.Errorf("parse peer multiaddr %q: %w", addr, err)
	}
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return peer.AddrInfo{}, fmt.Errorf("parse peer info %q: %w", addr, err)
	}
	return *info, nil
}

func ConnectAddrs(ctx context.Context, h corehost.Host, addrs []string) ([]peer.AddrInfo, error) {
	connected := make([]peer.AddrInfo, 0, len(addrs))
	for _, addr := range addrs {
		info, err := ParseAddrInfo(addr)
		if err != nil {
			return connected, err
		}
		if info.ID == h.ID() {
			continue
		}
		if err := Connect(ctx, h, info); err != nil {
			return connected, err
		}
		connected = append(connected, info)
	}
	return connected, nil
}
