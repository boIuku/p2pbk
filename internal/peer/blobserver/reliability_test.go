package blobserver

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"p2pbk/internal/peer/quota"
)

// P0-09: the offered space is a disk budget, not a per-process counter.
func TestQuotaSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	q, _ := quota.New(10)
	s, err := NewWithQuota(root, q)
	if err != nil {
		t.Fatal(err)
	}
	ref := ShardRef{Owner: "alice", BlobID: "one", Index: 0}
	if _, err = s.PutShard(ctx, PutShardRequest{Ref: ref, Data: []byte("12345678")}); err != nil {
		t.Fatal(err)
	}
	q2, _ := quota.New(10)
	restarted, err := NewWithQuota(root, q2)
	if err != nil {
		t.Fatal(err)
	}
	if q2.Total() != 8 {
		t.Fatalf("restart forgot durable usage: got %d, want 8", q2.Total())
	}
	ref.BlobID = "two"
	if _, err = restarted.PutShard(ctx, PutShardRequest{Ref: ref, Data: []byte("abc")}); !errors.Is(err, quota.ErrExceeded) {
		t.Fatalf("over-budget write: %v", err)
	}
}

func TestFailedShardWritePreservesBytesAndQuota(t *testing.T) {
	ctx := context.Background()
	q, _ := quota.New(100)
	s, err := NewWithQuota(t.TempDir(), q)
	if err != nil {
		t.Fatal(err)
	}
	ref := ShardRef{Owner: "alice", BlobID: "one", Index: 0}
	if _, err = s.PutShard(ctx, PutShardRequest{Ref: ref, Data: []byte("old")}); err != nil {
		t.Fatal(err)
	}
	diskFull := errors.New("injected disk full before replacement")
	s.writeFile = func(string, []byte, os.FileMode) error { return diskFull }
	if _, err = s.PutShard(ctx, PutShardRequest{Ref: ref, Data: []byte("larger replacement")}); !errors.Is(err, diskFull) {
		t.Fatalf("wrong failure: %v", err)
	}
	got, err := s.GetShard(ctx, GetShardRequest{Ref: ref})
	if err != nil || string(got.Data) != "old" || q.Total() != 3 {
		t.Fatalf("failed write changed old data/accounting: %q %d %v", got.Data, q.Total(), err)
	}
}

func TestZeroOfferRejectsRemoteWrites(t *testing.T) {
	q, _ := quota.New(0)
	s, err := NewWithQuota(t.TempDir(), q)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PutShard(context.Background(), PutShardRequest{Ref: ShardRef{Owner: "alice", BlobID: "one", Index: 0}, Data: []byte("x")})
	if !errors.Is(err, quota.ErrExceeded) {
		t.Fatalf("zero offer accepted data: %v", err)
	}
}

// Concurrent retries of one shard consume its size exactly once.
func TestConcurrentShardQuota(t *testing.T) {
	q, _ := quota.New(1000)
	s, err := NewWithQuota(t.TempDir(), q)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.PutShard(context.Background(), PutShardRequest{Ref: ShardRef{Owner: "alice", BlobID: "one", Index: 0}, Data: []byte("12345678")})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if q.Total() != 8 {
		t.Fatalf("usage=%d, want exactly one 8-byte shard", q.Total())
	}
}
