package node

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/kopia/kopia/repo/blob"
	corehost "github.com/libp2p/go-libp2p/core/host"

	"p2pbk/internal/backend/p2p/locator"
	"p2pbk/internal/config"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/localdb"
)

func TestOpenDistributedStorageWithLocatorUsesConfiguredPeers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("client host New returned error: %v", err)
	}
	defer client.Close()

	peerAddrs := startConfiguredBlobPeers(t, ctx, 3)
	cfg := config.Default()
	cfg.Node.DataDir = filepath.Join(t.TempDir(), "client")
	cfg.Network.BootstrapPeers = peerAddrs
	cfg.Network.MinPeers = 3
	cfg.Backup.DataShards = 2
	cfg.Backup.ParityShards = 1

	st, peers, err := OpenDistributedStorageWithLocator(ctx, cfg, client, locator.NewMemory())
	if err != nil {
		t.Fatalf("OpenDistributedStorageWithLocator returned error: %v", err)
	}
	if len(peers) != 3 {
		t.Fatalf("peers = %d, want 3", len(peers))
	}

	input := []byte("configured peers carry this blob")
	if err := st.PutBlob(ctx, "node/blob", byteBlob(input), blob.PutOptions{}); err != nil {
		t.Fatalf("PutBlob returned error: %v", err)
	}

	var out outputBuffer
	if err := st.GetBlob(ctx, "node/blob", 0, -1, &out); err != nil {
		t.Fatalf("GetBlob returned error: %v", err)
	}
	if !bytes.Equal(out.bytes, input) {
		t.Fatalf("output = %q, want %q", out.bytes, input)
	}
}

func TestShouldUseDistributedStorageWithSavedPeers(t *testing.T) {
	cfg := config.Default()
	cfg.Node.DataDir = filepath.Join(t.TempDir(), "client")
	cfg.Network.BootstrapPeers = nil

	distributed, err := ShouldUseDistributedStorage(cfg)
	if err != nil {
		t.Fatalf("ShouldUseDistributedStorage returned error: %v", err)
	}
	if distributed {
		t.Fatal("distributed = true before any peer is saved")
	}

	paths, err := cfg.Paths()
	if err != nil {
		t.Fatalf("Paths returned error: %v", err)
	}
	db, err := localdb.Open(paths.LocalDB)
	if err != nil {
		t.Fatalf("Open localdb returned error: %v", err)
	}
	if err := db.UpsertPeer(localdb.PeerRecord{
		ID:     "peer-a",
		Addrs:  []string{"/ip4/127.0.0.1/tcp/4001/p2p/peer-a"},
		Source: "mdns",
	}); err != nil {
		t.Fatalf("UpsertPeer returned error: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	distributed, err = ShouldUseDistributedStorage(cfg)
	if err != nil {
		t.Fatalf("ShouldUseDistributedStorage returned error after save: %v", err)
	}
	if !distributed {
		t.Fatal("distributed = false after saved peer")
	}
}

func startConfiguredBlobPeers(t *testing.T, ctx context.Context, count int) []string {
	t.Helper()

	addrs := make([]string, 0, count)
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

		addrs = append(addrs, firstP2PAddr(h))
	}
	return addrs
}

func firstP2PAddr(h corehost.Host) string {
	return h.Addrs()[0].String() + "/p2p/" + h.ID().String()
}

type byteBlob []byte

func (b byteBlob) Length() int {
	return len(b)
}

func (b byteBlob) Reader() io.ReadSeekCloser {
	return nopReadSeekCloser{Reader: bytes.NewReader(b)}
}

func (b byteBlob) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(b)
	return int64(n), err
}

type nopReadSeekCloser struct {
	*bytes.Reader
}

func (nopReadSeekCloser) Close() error {
	return nil
}

type outputBuffer struct {
	bytes []byte
}

func (o *outputBuffer) Reset() {
	o.bytes = nil
}

func (o *outputBuffer) Length() int {
	return len(o.bytes)
}

func (o *outputBuffer) Write(p []byte) (int, error) {
	o.bytes = append(o.bytes, p...)
	return len(p), nil
}
