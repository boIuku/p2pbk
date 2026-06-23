package p2p

import (
	"bytes"
	"context"
	"encoding/hex"
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

func TestDistributedStorageRecordsPeerLocationsForEachShard(t *testing.T) {
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
	loc := locator.NewMemory()
	backend, err := NewDistributedBackend(dispatcher.New(client), strategy, loc)
	if err != nil {
		t.Fatalf("NewDistributedBackend returned error: %v", err)
	}
	st, err := NewStorage(backend, Options{Owner: "owner", DataShards: 2, ParityShards: 1})
	if err != nil {
		t.Fatalf("NewStorage returned error: %v", err)
	}

	const blobID = blob.ID("located-blob")
	if err := st.PutBlob(ctx, blobID, testBytes("location metadata matters"), blob.PutOptions{}); err != nil {
		t.Fatalf("PutBlob returned error: %v", err)
	}

	locationKey := hex.EncodeToString([]byte(blobID))
	locations, err := loc.Get(ctx, locationKey)
	if err != nil {
		t.Fatalf("locator Get returned error: %v", err)
	}
	if len(locations) != 3 {
		t.Fatalf("locations = %d, want 3: %+v", len(locations), locations)
	}

	assignments, err := strategy.Place(3)
	if err != nil {
		t.Fatalf("strategy Place returned error: %v", err)
	}
	for i, location := range locations {
		if location.Ref.Owner != "owner" {
			t.Fatalf("location %d owner = %q, want owner", i, location.Ref.Owner)
		}
		if location.Ref.BlobID != locationKey {
			t.Fatalf("location %d blob = %q, want %q", i, location.Ref.BlobID, locationKey)
		}
		if location.Ref.Index != i {
			t.Fatalf("location %d index = %d, want %d", i, location.Ref.Index, i)
		}
		if location.Peer != assignments[i].Peer {
			t.Fatalf("location %d peer = %s, want %s", i, location.Peer, assignments[i].Peer)
		}
	}
}

func TestDistributedStorageUpdatesAndDeletesPeerLocations(t *testing.T) {
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
	loc := locator.NewMemory()
	backend, err := NewDistributedBackend(dispatcher.New(client), strategy, loc)
	if err != nil {
		t.Fatalf("NewDistributedBackend returned error: %v", err)
	}
	st, err := NewStorage(backend, Options{Owner: "owner", DataShards: 2, ParityShards: 1})
	if err != nil {
		t.Fatalf("NewStorage returned error: %v", err)
	}

	const blobID = blob.ID("rewrite-blob")
	locationKey := hex.EncodeToString([]byte(blobID))
	if err := st.PutBlob(ctx, blobID, testBytes("first payload"), blob.PutOptions{}); err != nil {
		t.Fatalf("first PutBlob returned error: %v", err)
	}
	if err := st.PutBlob(ctx, blobID, testBytes("second payload with different bytes"), blob.PutOptions{}); err != nil {
		t.Fatalf("second PutBlob returned error: %v", err)
	}
	locations, err := loc.Get(ctx, locationKey)
	if err != nil {
		t.Fatalf("locator Get returned error: %v", err)
	}
	if len(locations) != 3 {
		t.Fatalf("locations after rewrite = %d, want 3 without duplicates: %+v", len(locations), locations)
	}

	if err := st.DeleteBlob(ctx, blobID); err != nil {
		t.Fatalf("DeleteBlob returned error: %v", err)
	}
	locations, err = loc.Get(ctx, locationKey)
	if err != nil {
		t.Fatalf("locator Get after delete returned error: %v", err)
	}
	if len(locations) != 0 {
		t.Fatalf("locations after delete = %+v, want empty", locations)
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
