package p2p

import (
	"bytes"
	"context"
	"testing"

	"github.com/kopia/kopia/repo/blob"
	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"p2pbk/internal/backend/p2p/dispatcher"
	"p2pbk/internal/backend/p2p/locator"
	"p2pbk/internal/backend/p2p/placement"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
)

func TestDistributedStorageRoundTripAcrossPeers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("client host New returned error: %v", err)
	}
	defer client.Close()

	peers := startBlobPeers(t, ctx, client, 3)
	peerIDs := make([]peer.ID, 0, len(peers))
	for _, p := range peers {
		peerIDs = append(peerIDs, p.ID())
	}

	strategy, err := placement.NewRoundRobin(peerIDs)
	if err != nil {
		t.Fatalf("NewRoundRobin returned error: %v", err)
	}
	backend, err := NewDistributedBackend(dispatcher.New(client), strategy, locator.NewMemory())
	if err != nil {
		t.Fatalf("NewDistributedBackend returned error: %v", err)
	}
	st, err := NewStorage(backend, Options{Owner: "owner", DataShards: 2, ParityShards: 1})
	if err != nil {
		t.Fatalf("NewStorage returned error: %v", err)
	}

	input := []byte("this blob crosses real libp2p streams")
	if err := st.PutBlob(ctx, "distributed-blob", testBytes(input), blob.PutOptions{}); err != nil {
		t.Fatalf("PutBlob returned error: %v", err)
	}

	var out testOutput
	if err := st.GetBlob(ctx, "distributed-blob", 0, -1, &out); err != nil {
		t.Fatalf("GetBlob returned error: %v", err)
	}
	if !bytes.Equal(out.bytes, input) {
		t.Fatalf("output = %q, want %q", out.bytes, input)
	}
}

func TestDistributedStorageRecoversWhenOnePeerIsUnavailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("client host New returned error: %v", err)
	}
	defer client.Close()

	peers := startBlobPeers(t, ctx, client, 3)
	peerIDs := make([]peer.ID, 0, len(peers))
	for _, p := range peers {
		peerIDs = append(peerIDs, p.ID())
	}

	strategy, err := placement.NewRoundRobin(peerIDs)
	if err != nil {
		t.Fatalf("NewRoundRobin returned error: %v", err)
	}
	backend, err := NewDistributedBackend(dispatcher.New(client), strategy, locator.NewMemory())
	if err != nil {
		t.Fatalf("NewDistributedBackend returned error: %v", err)
	}
	st, err := NewStorage(backend, Options{Owner: "owner", DataShards: 2, ParityShards: 1})
	if err != nil {
		t.Fatalf("NewStorage returned error: %v", err)
	}

	input := []byte("two out of three shards are enough")
	if err := st.PutBlob(ctx, "resilient-blob", testBytes(input), blob.PutOptions{}); err != nil {
		t.Fatalf("PutBlob returned error: %v", err)
	}
	if err := peers[0].Close(); err != nil {
		t.Fatalf("peer Close returned error: %v", err)
	}

	var out testOutput
	if err := st.GetBlob(ctx, "resilient-blob", 0, -1, &out); err != nil {
		t.Fatalf("GetBlob returned error with one unavailable peer: %v", err)
	}
	if !bytes.Equal(out.bytes, input) {
		t.Fatalf("output = %q, want %q", out.bytes, input)
	}
}

func startBlobPeers(t *testing.T, ctx context.Context, client corehost.Host, count int) []corehost.Host {
	t.Helper()

	peers := make([]corehost.Host, 0, count)
	for i := 0; i < count; i++ {
		h, err := p2phost.New(ctx, p2phost.Options{})
		if err != nil {
			t.Fatalf("peer host New returned error: %v", err)
		}
		t.Cleanup(func() { _ = h.Close() })

		server, err := blobserver.New(t.TempDir())
		if err != nil {
			t.Fatalf("blobserver New returned error: %v", err)
		}
		router := wire.NewRouter()
		server.Register(router)
		router.Register(h)

		if err := p2phost.Connect(ctx, client, p2phost.AddrInfo(h)); err != nil {
			t.Fatalf("Connect returned error: %v", err)
		}
		peers = append(peers, h)
	}
	return peers
}
