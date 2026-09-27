package blobserver_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/localdb"
)

// M1.2: discovery/connectivity never grants storage admission. An owner must
// be approved before the authenticated put reaches the durable store.
func TestUnapprovedPeerCannotStoreShard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverHost, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer serverHost.Close()
	clientHost, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer clientHost.Close()

	db, err := localdb.Open(t.TempDir() + "/local.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	server, err := blobserver.NewWithOptions(t.TempDir(), blobserver.Options{Approvals: db})
	if err != nil {
		t.Fatal(err)
	}
	router := wire.NewRouter()
	server.Register(router)
	router.Register(serverHost)
	if err := p2phost.Connect(ctx, clientHost, p2phost.AddrInfo(serverHost)); err != nil {
		t.Fatal(err)
	}

	ref := blobserver.ShardRef{Owner: clientHost.ID().String(), BlobID: "blob", Index: 0}
	var response blobserver.PutShardResponse
	call := func() error {
		return wire.NewClient(clientHost).Call(ctx, serverHost.ID(), blobserver.MethodPutShard,
			blobserver.PutShardRequest{Ref: ref, Data: []byte("secret")}, &response)
	}
	if err := call(); !strings.Contains(errorString(err), blobserver.ErrPeerNotApproved.Error()) {
		t.Fatalf("unapproved put error = %v, want approval error", err)
	}
	if _, err := server.GetShard(ctx, blobserver.GetShardRequest{Ref: ref}); !errors.Is(err, blobserver.ErrShardNotFound) {
		t.Fatalf("unapproved put mutated storage: %v", err)
	}
	if err := db.ApprovePeer(ref.Owner, "machine-a", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := call(); err != nil {
		t.Fatalf("approved put failed: %v", err)
	}
	if err := db.RevokePeer(ref.Owner, "test revoke", time.Now()); err != nil {
		t.Fatal(err)
	}
	ref.BlobID = "new-blob"
	if err := call(); err == nil {
		t.Fatal("revoked owner stored a new shard")
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
