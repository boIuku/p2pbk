package integration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"p2pbk/internal/backend/p2p/locator"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/identity"
)

// P0-06: map fakes cannot prove DHT validation, propagation or retrieval. Stop
// the publisher before reading through an independent network node.
func TestSignedLocatorAcrossRealDHTNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var peers []*durablePeer
	for i := 0; i < 3; i++ {
		p := &durablePeer{dir: t.TempDir()}
		p.start(t, ctx)
		t.Cleanup(p.stop)
		peers = append(peers, p)
	}
	for i, p := range peers {
		for j, q := range peers {
			if i != j {
				if err := p.host.Connect(ctx, p2phost.AddrInfo(q.host)); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := p.dht.Bootstrap(ctx); err != nil {
			t.Fatal(err)
		}
	}
	id, err := identity.Create(filepath.Join(t.TempDir(), "identity.age"), "pass")
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := locator.NewDHT(peers[0].dht, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	location := locator.Location{Peer: peers[2].host.ID(), Ref: blobserver.ShardRef{Owner: peers[0].host.ID().String(), BlobID: "blob", Index: 0}}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = publisher.Put(ctx, "blob", []locator.Location{location})
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("DHT never became ready: %v", err)
		case <-ticker.C:
		}
	}
	peers[0].stop()
	reader, err := locator.NewDHT(peers[1].dht, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.Get(ctx, "blob")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != location {
		t.Fatalf("network locator changed: %+v", got)
	}
	if err := peers[1].dht.PutValue(ctx, "/p2pbk/locator/forged", []byte(`{"issued_at":"2099-01-01T00:00:00Z"}`)); err == nil {
		t.Fatal("DHT admitted unsigned future-dated metadata")
	}
}
