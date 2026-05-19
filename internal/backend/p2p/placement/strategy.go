package placement

import (
	"errors"
	"fmt"
	"sort"

	"github.com/libp2p/go-libp2p/core/peer"
)

var ErrNoPeers = errors.New("no peers available for placement")

type Assignment struct {
	ShardIndex int
	Peer       peer.ID
}

type Strategy interface {
	Place(totalShards int) ([]Assignment, error)
}

type RoundRobin struct {
	peers []peer.ID
}

func NewRoundRobin(peers []peer.ID) (*RoundRobin, error) {
	if len(peers) == 0 {
		return nil, ErrNoPeers
	}

	uniq := map[peer.ID]struct{}{}
	out := make([]peer.ID, 0, len(peers))
	for _, p := range peers {
		if p == "" {
			continue
		}
		if _, ok := uniq[p]; ok {
			continue
		}
		uniq[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, ErrNoPeers
	}

	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return &RoundRobin{peers: out}, nil
}

func (r *RoundRobin) Place(totalShards int) ([]Assignment, error) {
	if totalShards <= 0 {
		return nil, fmt.Errorf("totalShards must be positive")
	}
	if len(r.peers) == 0 {
		return nil, ErrNoPeers
	}

	assignments := make([]Assignment, totalShards)
	for i := range assignments {
		assignments[i] = Assignment{ShardIndex: i, Peer: r.peers[i%len(r.peers)]}
	}
	return assignments, nil
}
