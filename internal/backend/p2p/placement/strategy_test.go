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
