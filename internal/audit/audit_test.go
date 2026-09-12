package audit

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"p2pbk/internal/config"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/trust/receipts"
)

func TestRunOnceRecordsSuccessfulAudit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("client host New returned error: %v", err)
	}
	defer client.Close()

	storageHost, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("storage host New returned error: %v", err)
	}
	defer storageHost.Close()

	server, err := blobserver.New(t.TempDir())
	if err != nil {
		t.Fatalf("blobserver New returned error: %v", err)
	}
	ref := blobserver.ShardRef{Owner: client.ID().String(), BlobID: "blob", Index: 1}
	data := []byte("encrypted shard bytes")
	putResp, err := server.PutShard(ctx, blobserver.PutShardRequest{Ref: ref, Data: data})
	if err != nil {
		t.Fatalf("PutShard returned error: %v", err)
	}
	router := wire.NewRouter()
	server.Register(router)
	router.Register(storageHost)
	if err := p2phost.Connect(ctx, client, p2phost.AddrInfo(storageHost)); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	db, err := localdb.Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()
	if err := db.StoreShardReceipt(localdb.ShardReceiptRecord{
		PeerID: storageHost.ID().String(),
		Receipt: receipts.Receipt{
			Ref:       receipts.ShardRef{Owner: ref.Owner, BlobID: ref.BlobID, Index: ref.Index},
			Hash:      putResp.Receipt.Hash,
			StoredBy:  storageHost.ID().String(),
			IssuedAt:  time.Unix(1, 0),
			Signature: "unsigned-test",
		},
		StoredAt: time.Unix(1, 0),
	}); err != nil {
		t.Fatalf("StoreShardReceipt returned error: %v", err)
	}

	summary, err := RunOnce(ctx, Options{Config: config.Default(), Host: client, DB: db})
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if summary.Checked != 1 || summary.Succeeded != 1 || summary.Failed != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	stats, err := db.ListPeerStats()
	if err != nil {
		t.Fatalf("ListPeerStats returned error: %v", err)
	}
	if len(stats) != 1 || stats[0].AuditSuccesses != 1 || stats[0].Status != "healthy" {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestRunOnceHonorsLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	host, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("host New returned error: %v", err)
	}
	defer host.Close()

	db, err := localdb.Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	for i := 0; i < 3; i++ {
		ref := blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: i}
		if err := db.StoreShardReceipt(localdb.ShardReceiptRecord{
			PeerID: host.ID().String(),
			Receipt: receipts.Receipt{
				Ref:       receipts.ShardRef{Owner: ref.Owner, BlobID: ref.BlobID, Index: ref.Index},
				Hash:      "hash",
				StoredBy:  host.ID().String(),
				IssuedAt:  time.Unix(int64(i+1), 0),
				Signature: "unsigned-test",
			},
			StoredAt: time.Unix(int64(i+1), 0),
		}); err != nil {
			t.Fatalf("StoreShardReceipt returned error: %v", err)
		}
	}

	summary, err := RunOnce(ctx, Options{
		Config:       config.Default(),
		Host:         host,
		DB:           db,
		Limit:        1,
		ShardTimeout: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if summary.Checked != 1 {
		t.Fatalf("checked = %d, want 1", summary.Checked)
	}
}
