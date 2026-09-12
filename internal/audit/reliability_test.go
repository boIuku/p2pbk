package audit

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"p2pbk/internal/backend/p2p/dispatcher"
	"p2pbk/internal/config"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/localdb"
)

// Trust must follow verified data. A responsive peer serving corrupt data is not
// healthy; failed audit history must survive a local database reopen.
func TestAuditFailuresPersistAndReduceTrust(t *testing.T) {
	for _, failure := range []string{"corrupt", "missing"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			owner, err := p2phost.New(ctx, p2phost.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			h, err := p2phost.New(ctx, p2phost.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			server, err := blobserver.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			r := wire.NewRouter()
			server.Register(r)
			r.Register(h)
			if err = owner.Connect(ctx, p2phost.AddrInfo(h)); err != nil {
				t.Fatal(err)
			}
			ref := blobserver.ShardRef{Owner: owner.ID().String(), BlobID: "blob", Index: 0}
			resp, err := dispatcher.New(owner).PutShard(ctx, h.ID(), blobserver.PutShardRequest{Ref: ref, Data: []byte("original")})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "local.db")
			db, err := localdb.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = db.StoreShardReceipt(localdb.ShardReceiptRecord{PeerID: h.ID().String(), Receipt: resp.Receipt}); err != nil {
				t.Fatal(err)
			}
			opts := Options{Host: owner, DB: db, Config: config.Default(), Now: func() time.Time { return time.Unix(100, 0) }}
			good, err := RunOnce(ctx, opts)
			if err != nil || good.Succeeded != 1 {
				t.Fatalf("healthy baseline: %+v %v", good, err)
			}
			if failure == "missing" {
				_, err = server.DeleteShard(ctx, blobserver.DeleteShardRequest{Ref: ref})
			} else {
				_, err = server.PutShard(ctx, blobserver.PutShardRequest{Ref: ref, Data: []byte("corrupted")})
			}
			if err != nil {
				t.Fatal(err)
			}
			opts.Now = func() time.Time { return time.Unix(200, 0) }
			bad, err := RunOnce(ctx, opts)
			if err != nil || bad.Failed != 1 || bad.Succeeded != 0 {
				t.Fatalf("bad peer reported healthy: %+v %v", bad, err)
			}
			db.Close()
			db, err = localdb.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			stats, err := db.ListPeerStats()
			if err != nil {
				t.Fatal(err)
			}
			if len(stats) != 1 || stats[0].AuditFailures != 1 || stats[0].AuditSuccesses != 1 || stats[0].Score >= 1 {
				t.Fatalf("failure did not persist in trust: %+v", stats)
			}
			history, err := db.ListAuditHistory(h.ID().String())
			if err != nil {
				t.Fatal(err)
			}
			if len(history) != 2 || history[0].Success || history[0].Error == "" {
				t.Fatalf("audit evidence missing: %+v", history)
			}
		})
	}
}
