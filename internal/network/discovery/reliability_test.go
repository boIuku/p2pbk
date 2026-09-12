package discovery

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/peer/localdb"
)

func TestConnectionsContinuePastStaleAndMalformedPeers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	live, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	dead, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.Addrs()[0].String() + "/p2p/" + dead.ID().String()
	dead.Close()
	db, err := localdb.Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	liveAddr := live.Addrs()[0].String() + "/p2p/" + live.ID().String()
	got, err := ConnectConfiguredAndSaved(ctx, client, []string{"invalid", deadAddr, liveAddr, liveAddr}, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != live.ID() {
		t.Fatalf("stale peer prevented useful connection: %+v", got)
	}
	saved, err := db.ListPeers()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range saved {
		if p.ID == live.ID().String() {
			found = true
		}
	}
	if !found {
		t.Fatal("successful address was not persisted")
	}
}

type stalledRouter struct{ started chan struct{} }

func (r stalledRouter) Advertise(context.Context, string, time.Duration) (time.Duration, error) {
	return time.Second, nil
}
func (r stalledRouter) FindPeers(context.Context, string, int) (<-chan peer.AddrInfo, error) {
	close(r.started)
	return make(chan peer.AddrInfo), nil
}

func TestRemoteDiscoveryShutdownWithStalledProviderChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	router := stalledRouter{started: make(chan struct{})}
	service, err := StartRemote(ctx, h, nil, router, "test", WithRemoteRoundTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-router.started:
	case <-time.After(time.Second):
		t.Fatal("discovery never started")
	}
	done := make(chan struct{})
	go func() { service.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("discovery shutdown waits forever for provider channel")
	}
}
