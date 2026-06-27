package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/peer/localdb"
)

type fakeRemoteRouter struct {
	peers      []peer.AddrInfo
	advertised int
}

func (r *fakeRemoteRouter) Advertise(context.Context, string, time.Duration) (time.Duration, error) {
	r.advertised++
	return time.Minute, nil
}

func (r *fakeRemoteRouter) FindPeers(ctx context.Context, _ string, _ int) (<-chan peer.AddrInfo, error) {
	ch := make(chan peer.AddrInfo, len(r.peers))
	defer close(ch)
	for _, info := range r.peers {
		select {
		case <-ctx.Done():
			return ch, nil
		case ch <- info:
		}
	}
	return ch, nil
}

type recordingPeerStore struct {
	saved chan localdb.PeerRecord
}

func (s recordingPeerStore) UpsertPeer(record localdb.PeerRecord) error {
	s.saved <- record
	return nil
}

func (s recordingPeerStore) RecordPeerFailure(string, time.Time) error {
	return nil
}

func (s recordingPeerStore) ListPeers() ([]localdb.PeerRecord, error) {
	return nil, nil
}

func TestStartRemoteSkipsFoundPeerWithOnlyPrivateAddrs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h1, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}})
	if err != nil {
		t.Fatalf("host 1 New returned error: %v", err)
	}
	defer h1.Close()

	h2, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}})
	if err != nil {
		t.Fatalf("host 2 New returned error: %v", err)
	}
	defer h2.Close()

	store := recordingPeerStore{saved: make(chan localdb.PeerRecord, 1)}
	service, err := StartRemote(ctx, h1, store, &fakeRemoteRouter{peers: []peer.AddrInfo{p2phost.AddrInfo(h2)}}, "p2pbk/storage/v1", WithRemoteInterval(time.Hour))
	if err != nil {
		t.Fatalf("StartRemote returned error: %v", err)
	}
	defer service.Close()

	select {
	case record := <-store.saved:
		t.Fatalf("saved private-only peer: %+v", record)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestStartRemoteRejectsInvalidOptions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("host New returned error: %v", err)
	}
	defer h.Close()

	if _, err := StartRemote(ctx, h, nil, nil, "p2pbk/storage/v1"); err == nil {
		t.Fatal("StartRemote returned nil error for nil router")
	}
	if _, err := StartRemote(ctx, h, nil, &fakeRemoteRouter{}, ""); err == nil {
		t.Fatal("StartRemote returned nil error for empty namespace")
	}
	if _, err := StartRemote(ctx, h, nil, &fakeRemoteRouter{}, "p2pbk/storage/v1", WithRemoteInterval(0)); err == nil {
		t.Fatal("StartRemote returned nil error for zero interval")
	}
}

func TestRemoteDiscoverySkipsAdvertiseWhenRelayRequiredAndOnlyPrivateAddrs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}})
	if err != nil {
		t.Fatalf("host New returned error: %v", err)
	}
	defer h.Close()

	router := &fakeRemoteRouter{}
	runRemoteDiscoveryRound(ctx, h, router, "p2pbk/storage/v1", time.Minute, 10, &localNotifee{ctx: ctx, host: h}, PeerFilter{}, true)
	if router.advertised != 0 {
		t.Fatalf("advertised = %d, want 0", router.advertised)
	}
}

func TestRemoteDiscoveryAdvertisesWhenRelayAddressAvailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := p2phost.New(ctx, p2phost.Options{
		ListenAddrs:   []string{"/ip4/127.0.0.1/tcp/0"},
		AnnounceAddrs: []string{"/ip4/203.0.113.10/tcp/4001/p2p-circuit"},
	})
	if err != nil {
		t.Fatalf("host New returned error: %v", err)
	}
	defer h.Close()

	router := &fakeRemoteRouter{}
	runRemoteDiscoveryRound(ctx, h, router, "p2pbk/storage/v1", time.Minute, 10, &localNotifee{ctx: ctx, host: h}, PeerFilter{}, true)
	if router.advertised != 1 {
		t.Fatalf("advertised = %d, want 1", router.advertised)
	}
}

func TestRemoteDiscoveryFiltersIgnoredPeers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}})
	if err != nil {
		t.Fatalf("host New returned error: %v", err)
	}
	defer h.Close()

	ignored := peer.ID("ignored-peer")
	store := recordingPeerStore{saved: make(chan localdb.PeerRecord, 1)}
	router := &fakeRemoteRouter{peers: []peer.AddrInfo{{
		ID:    ignored,
		Addrs: []multiaddr.Multiaddr{mustMultiaddr(t, "/dns4/discovery.example.com/tcp/4001")},
	}}}
	runRemoteDiscoveryRound(ctx, h, router, "p2pbk/storage/v1", time.Minute, 10, &localNotifee{ctx: ctx, host: h, store: store}, NewPeerFilter([]peer.AddrInfo{{ID: ignored}}), false)

	select {
	case record := <-store.saved:
		t.Fatalf("saved ignored peer: %+v", record)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestUsefulRemoteAddrRequiresRelayWhenConfigured(t *testing.T) {
	privateAddr := mustMultiaddr(t, "/ip4/172.19.0.2/tcp/4001")
	relayAddr := mustMultiaddr(t, "/ip4/203.0.113.10/tcp/4001/p2p-circuit")
	if hasUsefulRemoteAddr([]multiaddr.Multiaddr{privateAddr}, true) {
		t.Fatal("private address was useful when relay is required")
	}
	if !hasUsefulRemoteAddr([]multiaddr.Multiaddr{relayAddr}, true) {
		t.Fatal("relay address was not useful when relay is required")
	}
}

func mustMultiaddr(t *testing.T, raw string) multiaddr.Multiaddr {
	t.Helper()
	addr, err := multiaddr.NewMultiaddr(raw)
	if err != nil {
		t.Fatalf("NewMultiaddr(%q): %v", raw, err)
	}
	return addr
}
