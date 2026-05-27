package reputation

import (
	"sort"
	"sync"
	"time"

	"p2pbk/internal/peer/blobserver"
)

type Event struct {
	Peer      string
	Success   bool
	Ref       blobserver.ShardRef
	Challenge string // nonce or identifier
	At        time.Time
}

type Tracker struct {
	mu     sync.Mutex
	window time.Duration
	events map[string][]Event
}

type Score struct {
	Peer      string
	Successes int
	Failures  int
	Score     float64
	Challenge string
}

func New(window time.Duration) *Tracker {
	if window <= 0 {
		window = 30 * 24 * time.Hour
	}
	return &Tracker{window: window, events: map[string][]Event{}}
}

// Record records a challenge event for a peer.
// This is called after a challenge response is verified (or fails).
func (t *Tracker) Record(peer, owner, blobID string, index uint32, success bool, nonce string, at time.Time) {
	if at.IsZero() {
		at = time.Now().UTC()
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.events[peer] = append(t.events[peer], Event{
		Peer:      peer,
		Success:   success,
		Ref:       blobserver.ShardRef{Owner: owner, BlobID: blobID, Index: int(index)},
		Challenge: nonce,
		At:        at.UTC(),
	})
	t.pruneLocked(peer, at.UTC())
}

// RecordSuccess records a successful challenge response.
func (t *Tracker) RecordSuccess(peer, owner, blobID string, index uint32, nonce string, at time.Time) {
	t.Record(peer, owner, blobID, index, true, nonce, at)
}

// RecordFailure records a failed challenge response.
func (t *Tracker) RecordFailure(peer, owner, blobID string, index uint32, nonce string, at time.Time) {
	t.Record(peer, owner, blobID, index, false, nonce, at)
}

func (t *Tracker) Score(peer string, now time.Time) Score {
	return t.ScoreForPeer(peer, now)
}

// ScoreForPeer returns the score for a specific peer across all their challenges.
func (t *Tracker) ScoreForPeer(peer string, now time.Time) Score {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked(peer, now.UTC())

	score := Score{Peer: peer}
	for _, event := range t.events[peer] {
		if event.Success {
			score.Successes++
			score.Challenge = event.Challenge
		} else {
			score.Failures++
		}
	}

	total := score.Successes + score.Failures
	if total == 0 {
		score.Score = 0.5
		return score
	}
	score.Score = float64(score.Successes) / float64(total)
	return score
}

// Ranked returns all peer scores, sorted by score.
func (t *Tracker) Ranked(now time.Time) []Score {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	t.mu.Lock()
	peers := make([]string, 0, len(t.events))
	for peer := range t.events {
		peers = append(peers, peer)
	}
	t.mu.Unlock()

	out := make([]Score, 0, len(peers))
	for _, peer := range peers {
		out = append(out, t.ScoreForPeer(peer, now))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Peer < out[j].Peer
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func (t *Tracker) pruneLocked(key string, now time.Time) {
	cutoff := now.Add(-t.window)
	events := t.events[key]
	kept := events[:0]
	for _, event := range events {
		if !event.At.Before(cutoff) {
			kept = append(kept, event)
		}
	}
	if len(kept) == 0 {
		delete(t.events, key)
		return
	}
	t.events[key] = kept
}
