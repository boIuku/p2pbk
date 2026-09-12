package host

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/gofrs/flock"
	"os"
	"p2pbk/internal/durable"
	"path/filepath"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

type Options struct {
	KeyPath            string
	ListenAddrs        []string
	AnnounceAddrs      []string
	PrivateKey         crypto.PrivKey
	StaticRelays       []peer.AddrInfo
	EnableNATPortMap   bool
	EnableRelayService bool
	EnableHolePunching bool
	ForcePrivateRelay  bool
}

func New(ctx context.Context, opts Options) (corehost.Host, error) {
	if opts.PrivateKey == nil && opts.KeyPath != "" {
		var err error
		opts.PrivateKey, err = LoadOrCreateKey(opts.KeyPath)
		if err != nil {
			return nil, err
		}
	}
	listenAddrs := opts.ListenAddrs
	if len(listenAddrs) == 0 {
		listenAddrs = []string{"/ip4/127.0.0.1/tcp/0"}
	}

	libp2pOpts := []libp2p.Option{
		libp2p.ListenAddrStrings(listenAddrs...),
	}
	if len(opts.AnnounceAddrs) > 0 {
		announceAddrs := make([]multiaddr.Multiaddr, 0, len(opts.AnnounceAddrs))
		for _, raw := range opts.AnnounceAddrs {
			addr, err := multiaddr.NewMultiaddr(raw)
			if err != nil {
				return nil, fmt.Errorf("parse announce multiaddr %q: %w", raw, err)
			}
			announceAddrs = append(announceAddrs, addr)
		}
		libp2pOpts = append(libp2pOpts, libp2p.AddrsFactory(func([]multiaddr.Multiaddr) []multiaddr.Multiaddr {
			return announceAddrs
		}))
	}
	if opts.PrivateKey != nil {
		libp2pOpts = append(libp2pOpts, libp2p.Identity(opts.PrivateKey))
	}
	if opts.EnableNATPortMap {
		libp2pOpts = append(libp2pOpts, libp2p.NATPortMap())
	}
	if len(opts.StaticRelays) > 0 {
		libp2pOpts = append(libp2pOpts, libp2p.EnableAutoRelayWithStaticRelays(opts.StaticRelays))
	}
	if opts.EnableRelayService {
		libp2pOpts = append(libp2pOpts, libp2p.EnableRelayService())
	}
	if opts.EnableHolePunching {
		libp2pOpts = append(libp2pOpts, libp2p.EnableHolePunching())
	}
	if opts.ForcePrivateRelay {
		libp2pOpts = append(libp2pOpts, libp2p.ForceReachabilityPrivate())
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

// LoadOrCreateKey preserves the identity in saved multiaddrs and shard receipts.
// A corrupt key is an error: silently generating a replacement strands old data.
func LoadOrCreateKey(path string) (crypto.PrivKey, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return nil, err
	}
	defer lock.Unlock()
	data, err := os.ReadFile(path)
	if err == nil {
		return crypto.UnmarshalPrivateKey(data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, err
	}
	data, err = crypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err = durable.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}
	return key, nil
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

func ParseAddrInfos(addrs []string) ([]peer.AddrInfo, error) {
	infos := make([]peer.AddrInfo, 0, len(addrs))
	for _, addr := range addrs {
		info, err := ParseAddrInfo(addr)
		if err != nil {
			return infos, err
		}
		infos = append(infos, info)
	}
	return infos, nil
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
