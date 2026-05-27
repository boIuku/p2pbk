package responder

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/challenges"
	"p2pbk/internal/trust/reputation"
)

func TestResponderAnswersChallenge(t *testing.T) {
	ctx := context.Background()
	server, err := blobserver.New(t.TempDir())
	if err != nil {
		t.Fatalf("blobserver New returned error: %v", err)
	}

	ref := blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 0}
	data := []byte("challenge me")
	if err := server.PutShard(ctx, blobserver.PutShardRequest{Ref: ref, Data: data}); err != nil {
		t.Fatalf("PutShard returned error: %v", err)
	}

	tracker := reputation.New(time.Hour)
	peerA := peer.ID("peer-a")
	r := New(server, nil, tracker, WithPeerID(peerA))
	ch, err := challenges.New(ref, int64(len(data)), 8, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("challenge New returned error: %v", err)
	}

	resp, err := r.Answer(ctx, ch)
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if err := challenges.Verify(ch, resp, data); err != nil {
		t.Fatalf("challenge Verify returned error: %v", err)
	}

	score := tracker.ScoreForPeer(peerA.String(), time.Now())
	if score.Successes != 1 || score.Failures != 0 {
		t.Fatalf("score = %+v, want one success", score)
	}
}

func TestResponderVerifyAndRecordFailure(t *testing.T) {
	server, err := blobserver.New(t.TempDir())
	if err != nil {
		t.Fatalf("blobserver New returned error: %v", err)
	}
	tracker := reputation.New(time.Hour)
	r := New(server, nil, tracker)

	peerB := peer.ID("peer-b")
	ok, err := r.VerifyAndRecord(context.Background(), "owner", "blob", 0, "abcd", "bad-proof", peerB, []byte("data"))
	if err != nil {
		t.Fatalf("VerifyAndRecord returned error: %v", err)
	}
	if ok {
		t.Fatal("VerifyAndRecord returned success for bad proof")
	}

	score := tracker.ScoreForPeer(peerB.String(), time.Now())
	if score.Successes != 0 || score.Failures != 1 {
		t.Fatalf("score = %+v, want one failure", score)
	}
}
