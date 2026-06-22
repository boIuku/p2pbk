package integration

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
	"p2pbk/internal/node"
	"p2pbk/internal/peer/blobserver"
)

func TestDistributedBackupStorageE2E(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("client host New returned error: %v", err)
	}
	defer client.Close()

	peerAddrs := startStoragePeers(t, ctx, 3)
	cfg := config.Default()
	cfg.Node.DataDir = filepath.Join(t.TempDir(), "client")
	cfg.Network.BootstrapPeers = peerAddrs
	cfg.Network.MinPeers = 3
	cfg.Backup.DataShards = 2
	cfg.Backup.ParityShards = 1

	st, _, err := node.OpenDistributedStorageWithLocator(ctx, cfg, client, locator.NewMemory())
	if err != nil {
		t.Fatalf("OpenDistributedStorageWithLocator returned error: %v", err)
	}

	input := []byte("e2e distributed backup payload")
	if err := st.PutBlob(ctx, "fixture/blob", blobBytes(input), blob.PutOptions{}); err != nil {
		t.Fatalf("PutBlob returned error: %v", err)
	}

	var out outputBuffer
	if err := st.GetBlob(ctx, "fixture/blob", 0, -1, &out); err != nil {
		t.Fatalf("GetBlob returned error: %v", err)
	}
	if !bytes.Equal(out.bytes, input) {
		t.Fatalf("restored payload = %q, want %q", out.bytes, input)
	}
}

func startStoragePeers(t *testing.T, ctx context.Context, count int) []string {
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

		addrs = append(addrs, p2pAddr(h))
	}
	return addrs
}

func p2pAddr(h corehost.Host) string {
	return h.Addrs()[0].String() + "/p2p/" + h.ID().String()
}

type blobBytes []byte

func (b blobBytes) Length() int {
	return len(b)
}

func (b blobBytes) Reader() io.ReadSeekCloser {
	return nopReadSeekCloser{Reader: bytes.NewReader(b)}
}

func (b blobBytes) WriteTo(w io.Writer) (int64, error) {
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
