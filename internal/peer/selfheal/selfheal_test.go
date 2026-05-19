package selfheal

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"p2pbk/internal/peer/blobserver"
)

func TestScannerReportsOKMissingAndCorrupt(t *testing.T) {
	root := t.TempDir()
	ok := blobserver.ShardRef{Owner: "owner", BlobID: "ok", Index: 0}
	missing := blobserver.ShardRef{Owner: "owner", BlobID: "missing", Index: 0}
	corrupt := blobserver.ShardRef{Owner: "owner", BlobID: "corrupt", Index: 0}

	writeShard(t, root, ok, []byte("ok"))
	writeShard(t, root, corrupt, []byte("bad"))

	results := Scanner{
		Root: root,
		Expected: []ExpectedShard{
			{Ref: ok, Hash: blobserver.Hash([]byte("ok"))},
			{Ref: missing, Hash: blobserver.Hash([]byte("missing"))},
			{Ref: corrupt, Hash: blobserver.Hash([]byte("good"))},
		},
	}.Scan(context.Background())

	if Count(results, StatusOK) != 1 {
		t.Fatalf("OK count = %d, want 1; results=%+v", Count(results, StatusOK), results)
	}
	if Count(results, StatusMissing) != 1 {
		t.Fatalf("missing count = %d, want 1; results=%+v", Count(results, StatusMissing), results)
	}
	if Count(results, StatusCorrupt) != 1 {
		t.Fatalf("corrupt count = %d, want 1; results=%+v", Count(results, StatusCorrupt), results)
	}
}

func TestScannerRepairsMissingShard(t *testing.T) {
	root := t.TempDir()
	ref := blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 0}
	expected := ExpectedShard{Ref: ref, Hash: blobserver.Hash([]byte("repaired"))}

	results := Scanner{
		Root:     root,
		Expected: []ExpectedShard{expected},
		Repair: func(ctx context.Context, shard ExpectedShard) error {
			writeShard(t, root, shard.Ref, []byte("repaired"))
			return nil
		},
	}.Scan(context.Background())

	if len(results) != 1 || results[0].Status != StatusRepaired {
		t.Fatalf("results = %+v, want repaired", results)
	}

	data, err := os.ReadFile(mustShardPath(t, root, ref))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(data) != "repaired" {
		t.Fatalf("data = %q, want repaired", data)
	}
}

func writeShard(t *testing.T, root string, ref blobserver.ShardRef, data []byte) {
	t.Helper()

	path := mustShardPath(t, root, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func mustShardPath(t *testing.T, root string, ref blobserver.ShardRef) string {
	t.Helper()

	path, err := blobserver.ShardPath(root, ref)
	if err != nil {
		t.Fatalf("ShardPath returned error: %v", err)
	}
	return path
}
