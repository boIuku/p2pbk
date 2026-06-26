package discovery

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"p2pbk/internal/peer/localdb"
)

func TestSanitizeRegistryName(t *testing.T) {
	got := sanitizeRegistryName(`_p2pbk/:\*?"<>|._udp`)
	if got != "_p2pbk_________._udp" {
		t.Fatalf("sanitizeRegistryName = %q", got)
	}
}

func TestParseMultiaddrsSkipsInvalidValues(t *testing.T) {
	addrs := parseMultiaddrs([]string{
		"/ip4/127.0.0.1/tcp/4001",
		"not-a-multiaddr",
	})
	if len(addrs) != 1 {
		t.Fatalf("parseMultiaddrs returned %d addrs, want 1", len(addrs))
	}
}

func TestFileRegistryDiscoversAndSavesLocalPeers(t *testing.T) {
	t.Setenv("P2PBK_LOCAL_DISCOVERY_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())

	h1, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatalf("create host 1: %v", err)
	}
	defer h1.Close()
	h2, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatalf("create host 2: %v", err)
	}
	defer h2.Close()

	db1, err := localdb.Open(filepath.Join(t.TempDir(), "node1.db"))
	if err != nil {
		t.Fatalf("open db 1: %v", err)
	}
	defer db1.Close()
	db2, err := localdb.Open(filepath.Join(t.TempDir(), "node2.db"))
	if err != nil {
		t.Fatalf("open db 2: %v", err)
	}
	defer db2.Close()

	s1 := &LocalService{}
	s1.startFileRegistry(ctx, h1, "test", &localNotifee{ctx: ctx, host: h1, store: db1})
	s2 := &LocalService{}
	s2.startFileRegistry(ctx, h2, "test", &localNotifee{ctx: ctx, host: h2, store: db2})

	requirePeerSaved(t, db1, h2.ID().String())
	requirePeerSaved(t, db2, h1.ID().String())

	cancel()
	if err := s1.Close(); err != nil {
		t.Fatalf("close service 1: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("close service 2: %v", err)
	}
}

func requirePeerSaved(t *testing.T, db *localdb.DB, peerID string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		peers, err := db.ListPeers()
		if err != nil {
			t.Fatalf("list peers: %v", err)
		}
		for _, record := range peers {
			if record.ID == peerID {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("peer %s was not saved", peerID)
}
