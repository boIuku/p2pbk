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

type CandidateStrategy interface {
	Candidates(shardIndex int) []peer.ID
}

type RoundRobin struct {
	peers []peer.ID
}

// FailureDomainRoundRobin spreads assignments across declared domains and
// refuses a placement that cannot leave enough shards after any one domain is
// lost. A missing domain is intentionally not invented by this strategy; the
// caller must either provide topology or use the legacy peer-only strategy.
type FailureDomainRoundRobin struct {
	peers         []peer.ID
	domains       map[peer.ID]string
	ordered       []string
	peersByDomain map[string][]peer.ID
	maxPerDomain  int
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

func NewScoredRoundRobin(peers []peer.ID, scores map[peer.ID]float64) (*RoundRobin, error) {
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
	sort.Slice(out, func(i, j int) bool {
		left := scores[out[i]]
		right := scores[out[j]]
		if left == right {
			return out[i].String() < out[j].String()
		}
		return left > right
	})
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

func (r *RoundRobin) Candidates(shardIndex int) []peer.ID {
	if len(r.peers) == 0 {
		return nil
	}
	start := shardIndex % len(r.peers)
	out := make([]peer.ID, 0, len(r.peers))
	for i := range r.peers {
		out = append(out, r.peers[(start+i)%len(r.peers)])
	}
	return out
}

func NewFailureDomainRoundRobin(peers []peer.ID, domains map[peer.ID]string, maxPerDomain int) (*FailureDomainRoundRobin, error) {
	if len(peers) == 0 {
		return nil, ErrNoPeers
	}
	if maxPerDomain <= 0 {
		return nil, fmt.Errorf("maxPerDomain must be positive")
	}
	uniq := map[peer.ID]struct{}{}
	byDomain := map[string][]peer.ID{}
	for _, p := range peers {
		if p == "" {
			continue
		}
		if _, ok := uniq[p]; ok {
			continue
		}
		domain := domains[p]
		if domain == "" {
			return nil, fmt.Errorf("failure domain missing for peer %s", p)
		}
		uniq[p] = struct{}{}
		byDomain[domain] = append(byDomain[domain], p)
	}
	if len(byDomain) == 0 {
		return nil, ErrNoPeers
	}
	ordered := make([]string, 0, len(byDomain))
	for domain, domainPeers := range byDomain {
		sort.Slice(domainPeers, func(i, j int) bool { return domainPeers[i].String() < domainPeers[j].String() })
		ordered = append(ordered, domain)
	}
	sort.Strings(ordered)
	return &FailureDomainRoundRobin{peers: append([]peer.ID(nil), peers...), domains: domains, ordered: ordered, peersByDomain: byDomain, maxPerDomain: maxPerDomain}, nil
}

func (r *FailureDomainRoundRobin) Place(totalShards int) ([]Assignment, error) {
	if totalShards <= 0 {
		return nil, fmt.Errorf("totalShards must be positive")
	}
	requiredDomains := (totalShards + r.maxPerDomain - 1) / r.maxPerDomain
	if len(r.ordered) < requiredDomains {
		return nil, fmt.Errorf("insufficient failure domains: have %d, require %d for %d shards", len(r.ordered), requiredDomains, totalShards)
	}
	assignments := make([]Assignment, totalShards)
	nextPeer := map[string]int{}
	for index := range assignments {
		domain := r.ordered[index%len(r.ordered)]
		peers := r.peersByDomain[domain]
		peerIndex := nextPeer[domain] % len(peers)
		assignments[index] = Assignment{ShardIndex: index, Peer: peers[peerIndex]}
		nextPeer[domain]++
	}
	return assignments, nil
}

func (r *FailureDomainRoundRobin) Candidates(shardIndex int) []peer.ID {
	if len(r.ordered) == 0 {
		return nil
	}
	domain := r.ordered[shardIndex%len(r.ordered)]
	return append([]peer.ID(nil), r.peersByDomain[domain]...)
}
