package reputation

import (
	"sort"
	"sync"
	"time"
)

type Event struct {
	Peer      string
	Succeeded bool
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
}

func New(window time.Duration) *Tracker {
	if window <= 0 {
		window = 30 * 24 * time.Hour
	}
	return &Tracker{window: window, events: map[string][]Event{}}
}

func (t *Tracker) Record(peer string, succeeded bool, at time.Time) {
	if at.IsZero() {
		at = time.Now().UTC()
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.events[peer] = append(t.events[peer], Event{Peer: peer, Succeeded: succeeded, At: at.UTC()})
	t.pruneLocked(peer, at.UTC())
}

func (t *Tracker) Score(peer string, now time.Time) Score {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked(peer, now.UTC())

	score := Score{Peer: peer}
	for _, event := range t.events[peer] {
		if event.Succeeded {
			score.Successes++
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
		out = append(out, t.Score(peer, now))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Peer < out[j].Peer
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func (t *Tracker) pruneLocked(peer string, now time.Time) {
	cutoff := now.Add(-t.window)
	events := t.events[peer]
	kept := events[:0]
	for _, event := range events {
		if !event.At.Before(cutoff) {
			kept = append(kept, event)
		}
	}
	if len(kept) == 0 {
		delete(t.events, peer)
		return
	}
	t.events[peer] = kept
}
