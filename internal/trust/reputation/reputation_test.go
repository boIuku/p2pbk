package reputation

import (
	"testing"
	"time"
)

func TestTrackerScoresPeerAcrossChallenges(t *testing.T) {
	now := time.Unix(100, 0)
	tracker := New(time.Hour)
	tracker.RecordSuccess("peer-a", "owner", "blob-1", 0, "n1", now)
	tracker.RecordFailure("peer-a", "owner", "blob-2", 1, "n2", now.Add(time.Minute))
	tracker.RecordSuccess("peer-a", "owner", "blob-3", 2, "n3", now.Add(2*time.Minute))

	score := tracker.ScoreForPeer("peer-a", now.Add(3*time.Minute))
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
	tracker.RecordFailure("peer-a", "owner", "old", 0, "n1", now.Add(-2*time.Hour))
	tracker.RecordSuccess("peer-a", "owner", "new", 0, "n2", now)

	score := tracker.ScoreForPeer("peer-a", now)
	if score.Successes != 1 || score.Failures != 0 || score.Score != 1 {
		t.Fatalf("score after prune = %+v", score)
	}
}

func TestRankedOrdersByPeerScore(t *testing.T) {
	now := time.Unix(100, 0)
	tracker := New(time.Hour)
	tracker.RecordFailure("peer-b", "owner", "blob", 0, "n1", now)
	tracker.RecordSuccess("peer-a", "owner", "blob", 0, "n2", now)

	ranked := tracker.Ranked(now)
	if len(ranked) != 2 || ranked[0].Peer != "peer-a" {
		t.Fatalf("ranked = %+v", ranked)
	}
}
