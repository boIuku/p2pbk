package discovery

import (
	"context"
	"fmt"
	"time"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"

	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/peer/localdb"
)

type PeerStore interface {
	UpsertPeer(localdb.PeerRecord) error
	RecordPeerFailure(string, time.Time) error
	ListPeers() ([]localdb.PeerRecord, error)
}

type LocalService struct {
	service mdns.Service
}

func StartLocal(ctx context.Context, h corehost.Host, store PeerStore, serviceName string) (*LocalService, error) {
	notifee := &localNotifee{ctx: ctx, host: h, store: store}
	service := mdns.NewMdnsService(h, serviceName, notifee)
	if err := service.Start(); err != nil {
		return nil, fmt.Errorf("start mdns discovery: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = service.Close()
	}()
	return &LocalService{service: service}, nil
}

func (s *LocalService) Close() error {
	if s == nil || s.service == nil {
		return nil
	}
	return s.service.Close()
}

func ConnectConfiguredAndSaved(ctx context.Context, h corehost.Host, configured []string, store PeerStore) ([]peer.AddrInfo, error) {
	addrs := append([]string(nil), configured...)
	if store != nil {
		records, err := store.ListPeers()
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			addrs = append(addrs, record.Addrs...)
		}
	}
	addrs = uniqueStrings(addrs)

	connected := make([]peer.AddrInfo, 0, len(addrs))
	for _, addr := range addrs {
		info, err := p2phost.ParseAddrInfo(addr)
		if err != nil {
			continue
		}
		if info.ID == h.ID() {
			continue
		}
		if err := p2phost.Connect(ctx, h, info); err != nil {
			if store != nil {
				_ = store.RecordPeerFailure(info.ID.String(), time.Now().UTC())
			}
			continue
		}
		connected = append(connected, info)
		if store != nil {
			_ = SavePeerInfo(store, info, "connected")
		}
	}
	return connected, nil
}

func SaveConnectedPeers(store PeerStore, h corehost.Host, source string) error {
	if store == nil {
		return nil
	}
	for _, id := range h.Network().Peers() {
		if id == "" || id == h.ID() {
			continue
		}
		info := peer.AddrInfo{ID: id, Addrs: h.Peerstore().Addrs(id)}
		if err := SavePeerInfo(store, info, source); err != nil {
			return err
		}
	}
	return nil
}

func SavePeerInfo(store PeerStore, info peer.AddrInfo, source string) error {
	if store == nil || info.ID == "" {
		return nil
	}
	addrs, err := peer.AddrInfoToP2pAddrs(&info)
	if err != nil {
		return err
	}
	raw := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		raw = append(raw, addr.String())
	}
	return store.UpsertPeer(localdb.PeerRecord{
		ID:       info.ID.String(),
		Addrs:    raw,
		Source:   source,
		LastSeen: time.Now().UTC(),
	})
}

type localNotifee struct {
	ctx   context.Context
	host  corehost.Host
	store PeerStore
}

func (n *localNotifee) HandlePeerFound(info peer.AddrInfo) {
	if info.ID == "" || info.ID == n.host.ID() {
		return
	}
	if err := p2phost.Connect(n.ctx, n.host, info); err != nil {
		if n.store != nil {
			_ = n.store.RecordPeerFailure(info.ID.String(), time.Now().UTC())
		}
		return
	}
	_ = SavePeerInfo(n.store, info, "mdns")
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
