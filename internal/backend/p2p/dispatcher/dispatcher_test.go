package dispatcher

import (
	"bytes"
	"context"
	"testing"

	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
)

func TestDispatcherTransfersShardToPeer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clientHost, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("client host New returned error: %v", err)
	}
	defer clientHost.Close()

	serverHost, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("server host New returned error: %v", err)
	}
	defer serverHost.Close()

	server, err := blobserver.New(t.TempDir())
	if err != nil {
		t.Fatalf("blobserver New returned error: %v", err)
	}
	router := wire.NewRouter()
	server.Register(router)
	router.Register(serverHost)

	if err := p2phost.Connect(ctx, clientHost, p2phost.AddrInfo(serverHost)); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	ref := blobserver.ShardRef{Owner: clientHost.ID().String(), BlobID: "blob-1", Index: 0}
	data := []byte("hello from dispatcher")
	d := New(clientHost)

	if err := d.PutShard(ctx, serverHost.ID(), blobserver.PutShardRequest{Ref: ref, Data: data}); err != nil {
		t.Fatalf("PutShard returned error: %v", err)
	}

	got, err := d.GetShard(ctx, serverHost.ID(), blobserver.GetShardRequest{Ref: ref})
	if err != nil {
		t.Fatalf("GetShard returned error: %v", err)
	}
	if !bytes.Equal(got.Data, data) {
		t.Fatalf("downloaded data = %q, want %q", got.Data, data)
	}

	deleted, err := d.DeleteShard(ctx, serverHost.ID(), blobserver.DeleteShardRequest{Ref: ref})
	if err != nil {
		t.Fatalf("DeleteShard returned error: %v", err)
	}
	if !deleted.Deleted {
		t.Fatal("DeleteShard reported no deletion")
	}
}
