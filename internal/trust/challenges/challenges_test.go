package challenges

import (
	"testing"
	"time"

	"p2pbk/internal/peer/blobserver"
)

func TestChallengeAnswerVerify(t *testing.T) {
	ref := blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 0}
	shard := []byte("abcdefghijklmnopqrstuvwxyz")
	ch, err := New(ref, int64(len(shard)), 8, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	resp, err := Answer(ch, shard)
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if err := Verify(ch, resp, shard); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}

func TestChallengeVerifyRejectsTamperedShard(t *testing.T) {
	ref := blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 0}
	shard := []byte("abcdefghijklmnopqrstuvwxyz")
	ch, err := New(ref, int64(len(shard)), 8, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	resp, err := Answer(ch, shard)
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}

	tampered := append([]byte(nil), shard...)
	tampered[int(ch.Offset)] ^= 0xff
	if err := Verify(ch, resp, tampered); err == nil {
		t.Fatal("Verify returned nil error for tampered shard")
	}
}

func TestIssuerDefaultsMaxLength(t *testing.T) {
	ref := blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 0}
	ch, err := (Issuer{Now: func() time.Time { return time.Unix(1, 0) }}).Issue(ref, 10)
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}
	if ch.Length != 10 {
		t.Fatalf("length = %d, want 10", ch.Length)
	}
}

func TestChallengeSupportsEmptyShard(t *testing.T) {
	ref := blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 0}
	ch, err := New(ref, 0, 8, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	resp, err := Answer(ch, nil)
	if err != nil {
		t.Fatalf("Answer returned error: %v", err)
	}
	if err := Verify(ch, resp, nil); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}
