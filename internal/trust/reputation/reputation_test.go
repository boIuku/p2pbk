package reputation

import (
	"testing"
	"time"
)

func TestTrackerScoresPeer(t *testing.T) {
	now := time.Unix(100, 0)
	tracker := New(time.Hour)
	tracker.Record("peer-a", true, now)
	tracker.Record("peer-a", false, now.Add(time.Minute))
	tracker.Record("peer-a", true, now.Add(2*time.Minute))

	score := tracker.Score("peer-a", now.Add(3*time.Minute))
	if score.Successes != 2 || score.Failures != 1 {
		t.Fatalf("score counts = %+v", score)
	}
	if score.Score < 0.66 || score.Score > 0.67 {
		t.Fatalf("score = %f, want about 0.667", score.Score)
	}
}

func TestTrackerPrunesOldEvents(t *testing.T) {
	now := time.Unix(1000, 0)
	tracker := New(time.Hour)
	tracker.Record("peer-a", false, now.Add(-2*time.Hour))
	tracker.Record("peer-a", true, now)

	score := tracker.Score("peer-a", now)
	if score.Successes != 1 || score.Failures != 0 || score.Score != 1 {
		t.Fatalf("score after prune = %+v", score)
	}
}

func TestRankedOrdersByScore(t *testing.T) {
	now := time.Unix(100, 0)
	tracker := New(time.Hour)
	tracker.Record("peer-b", false, now)
	tracker.Record("peer-a", true, now)

	ranked := tracker.Ranked(now)
	if len(ranked) != 2 || ranked[0].Peer != "peer-a" {
		t.Fatalf("ranked = %+v", ranked)
	}
}
