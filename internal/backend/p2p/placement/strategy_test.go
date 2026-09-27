package placement

import (
	"errors"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestRoundRobinPlacesShardsAcrossPeers(t *testing.T) {
	peers := []peer.ID{"peer-b", "peer-a"}
	strategy, err := NewRoundRobin(peers)
	if err != nil {
		t.Fatalf("NewRoundRobin returned error: %v", err)
	}

	assignments, err := strategy.Place(5)
	if err != nil {
		t.Fatalf("Place returned error: %v", err)
	}
	if len(assignments) != 5 {
		t.Fatalf("assignment count = %d, want 5", len(assignments))
	}
	if assignments[0].Peer != "peer-a" || assignments[1].Peer != "peer-b" || assignments[2].Peer != "peer-a" {
		t.Fatalf("unexpected peer sequence: %+v", assignments)
	}
}

func TestRoundRobinRejectsNoPeers(t *testing.T) {
	if _, err := NewRoundRobin(nil); !errors.Is(err, ErrNoPeers) {
		t.Fatalf("NewRoundRobin error = %v, want ErrNoPeers", err)
	}
}

func TestPlacementHonorsFailureDomains(t *testing.T) {
	peers := []peer.ID{"a", "b", "c", "d"}
	domains := map[peer.ID]string{"a": "host-a", "b": "host-b", "c": "host-c", "d": "host-d"}
	strategy, err := NewFailureDomainRoundRobin(peers, domains, 3)
	if err != nil {
		t.Fatal(err)
	}
	assignments, err := strategy.Place(9)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, assignment := range assignments {
		counts[domains[assignment.Peer]]++
	}
	for domain, count := range counts {
		if count > 3 {
			t.Fatalf("domain %s received %d shards, want at most 3", domain, count)
		}
	}
	if _, err := strategy.Place(13); err == nil {
		t.Fatal("Place(13) unexpectedly accepted a one-domain-loss-unsafe layout")
	}
}

func TestPlacementRejectsInsufficientFailureDomains(t *testing.T) {
	peers := []peer.ID{"a", "b"}
	domains := map[peer.ID]string{"a": "host-a", "b": "host-b"}
	strategy, err := NewFailureDomainRoundRobin(peers, domains, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strategy.Place(9); err == nil {
		t.Fatal("Place unexpectedly accepted two domains for nine shards")
	}
}
