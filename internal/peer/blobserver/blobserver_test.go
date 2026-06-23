package blobserver

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"p2pbk/internal/peer/quota"
)

func TestPutGetDeleteShard(t *testing.T) {
	server := newTestServer(t)
	ref := ShardRef{Owner: "owner-a", BlobID: "blob-a", Index: 2}
	data := []byte("stored shard data")

	putResp, err := server.PutShard(context.Background(), PutShardRequest{Ref: ref, Data: data, Hash: Hash(data)})
	if err != nil {
		t.Fatalf("PutShard returned error: %v", err)
	}
	if putResp.Receipt.Hash != Hash(data) || putResp.Receipt.Ref.BlobID != ref.BlobID {
		t.Fatalf("receipt = %+v", putResp.Receipt)
	}

	got, err := server.GetShard(context.Background(), GetShardRequest{Ref: ref})
	if err != nil {
		t.Fatalf("GetShard returned error: %v", err)
	}
	if !bytes.Equal(got.Data, data) {
		t.Fatalf("data = %q, want %q", got.Data, data)
	}
	if got.Hash != Hash(data) {
		t.Fatalf("hash = %q, want %q", got.Hash, Hash(data))
	}

	deleted, err := server.DeleteShard(context.Background(), DeleteShardRequest{Ref: ref})
	if err != nil {
		t.Fatalf("DeleteShard returned error: %v", err)
	}
	if !deleted.Deleted {
		t.Fatal("DeleteShard reported no deletion")
	}
	if _, err := server.GetShard(context.Background(), GetShardRequest{Ref: ref}); !errors.Is(err, ErrShardNotFound) {
		t.Fatalf("GetShard error = %v, want ErrShardNotFound", err)
	}
}

func TestPutShardRejectsHashMismatch(t *testing.T) {
	server := newTestServer(t)
	ref := ShardRef{Owner: "owner-a", BlobID: "blob-a", Index: 0}

	if _, err := server.PutShard(context.Background(), PutShardRequest{Ref: ref, Data: []byte("data"), Hash: Hash([]byte("other"))}); err == nil {
		t.Fatal("PutShard returned nil error for hash mismatch")
	}
}

func TestPutShardEnforcesQuota(t *testing.T) {
	q, err := quota.New(4)
	if err != nil {
		t.Fatalf("quota New returned error: %v", err)
	}
	server, err := NewWithQuota(t.TempDir(), q)
	if err != nil {
		t.Fatalf("NewWithQuota returned error: %v", err)
	}
	ref := ShardRef{Owner: "owner-a", BlobID: "blob-a", Index: 0}

	_, err = server.PutShard(context.Background(), PutShardRequest{Ref: ref, Data: []byte("12345")})
	if !errors.Is(err, quota.ErrExceeded) {
		t.Fatalf("PutShard error = %v, want ErrExceeded", err)
	}

	if _, err := server.PutShard(context.Background(), PutShardRequest{Ref: ref, Data: []byte("1234")}); err != nil {
		t.Fatalf("PutShard under quota returned error: %v", err)
	}
	if got := q.Used("owner-a"); got != 4 {
		t.Fatalf("used = %d, want 4", got)
	}

	deleted, err := server.DeleteShard(context.Background(), DeleteShardRequest{Ref: ref})
	if err != nil {
		t.Fatalf("DeleteShard returned error: %v", err)
	}
	if !deleted.Deleted {
		t.Fatal("DeleteShard reported false")
	}
	if got := q.Used("owner-a"); got != 0 {
		t.Fatalf("used after delete = %d, want 0", got)
	}
}

func TestShardPathRejectsTraversal(t *testing.T) {
	_, err := ShardPath(t.TempDir(), ShardRef{Owner: "..", BlobID: "blob", Index: 0})
	if err == nil {
		t.Fatal("ShardPath returned nil error for traversal owner")
	}

	_, err = ShardPath(t.TempDir(), ShardRef{Owner: "owner", BlobID: `a\b`, Index: 0})
	if err == nil {
		t.Fatal("ShardPath returned nil error for path separator")
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	server, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	return server
}
