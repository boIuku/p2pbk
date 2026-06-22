package p2p

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/kopia/kopia/repo/blob"

	"p2pbk/internal/peer/blobserver"
)

func TestStoragePutGetRoundTrip(t *testing.T) {
	st := newTestStorage(t)
	input := testBytes("hello p2p storage")

	var modTime time.Time
	if err := st.PutBlob(context.Background(), "blob-1", input, blob.PutOptions{GetModTime: &modTime}); err != nil {
		t.Fatalf("PutBlob returned error: %v", err)
	}
	if modTime.IsZero() {
		t.Fatal("PutBlob did not populate mod time")
	}

	var out testOutput
	if err := st.GetBlob(context.Background(), "blob-1", 0, -1, &out); err != nil {
		t.Fatalf("GetBlob returned error: %v", err)
	}
	if !bytes.Equal(out.bytes, []byte("hello p2p storage")) {
		t.Fatalf("output = %q", out.bytes)
	}
}

func TestStorageGetBlobRange(t *testing.T) {
	st := newTestStorage(t)
	if err := st.PutBlob(context.Background(), "blob-1", testBytes("abcdef"), blob.PutOptions{}); err != nil {
		t.Fatalf("PutBlob returned error: %v", err)
	}

	var out testOutput
	if err := st.GetBlob(context.Background(), "blob-1", 2, 3, &out); err != nil {
		t.Fatalf("GetBlob returned error: %v", err)
	}
	if string(out.bytes) != "cde" {
		t.Fatalf("range output = %q, want cde", out.bytes)
	}
}

func TestStorageListAndMetadata(t *testing.T) {
	st := newTestStorage(t)
	if err := st.PutBlob(context.Background(), "a/1", testBytes("one"), blob.PutOptions{}); err != nil {
		t.Fatalf("PutBlob a/1 returned error: %v", err)
	}
	if err := st.PutBlob(context.Background(), "b/1", testBytes("two"), blob.PutOptions{}); err != nil {
		t.Fatalf("PutBlob b/1 returned error: %v", err)
	}

	md, err := st.GetMetadata(context.Background(), "a/1")
	if err != nil {
		t.Fatalf("GetMetadata returned error: %v", err)
	}
	if md.Length != 3 {
		t.Fatalf("metadata length = %d, want 3", md.Length)
	}

	var ids []string
	err = st.ListBlobs(context.Background(), "a/", func(md blob.Metadata) error {
		ids = append(ids, string(md.BlobID))
		return nil
	})
	if err != nil {
		t.Fatalf("ListBlobs returned error: %v", err)
	}
	if len(ids) != 1 || ids[0] != "a/1" {
		t.Fatalf("ids = %v, want [a/1]", ids)
	}
}

func TestStorageDeleteBlob(t *testing.T) {
	st := newTestStorage(t)
	if err := st.PutBlob(context.Background(), "blob-1", testBytes("data"), blob.PutOptions{}); err != nil {
		t.Fatalf("PutBlob returned error: %v", err)
	}
	if err := st.DeleteBlob(context.Background(), "blob-1"); err != nil {
		t.Fatalf("DeleteBlob returned error: %v", err)
	}
	if _, err := st.GetMetadata(context.Background(), "blob-1"); !errors.Is(err, blob.ErrBlobNotFound) {
		t.Fatalf("GetMetadata error = %v, want ErrBlobNotFound", err)
	}
}

func TestStorageDoNotRecreate(t *testing.T) {
	st := newTestStorage(t)
	if err := st.PutBlob(context.Background(), "blob-1", testBytes("data"), blob.PutOptions{}); err != nil {
		t.Fatalf("PutBlob returned error: %v", err)
	}
	err := st.PutBlob(context.Background(), "blob-1", testBytes("again"), blob.PutOptions{DoNotRecreate: true})
	if !errors.Is(err, blob.ErrBlobAlreadyExists) {
		t.Fatalf("PutBlob error = %v, want ErrBlobAlreadyExists", err)
	}
}

func TestStorageRecoversWithMissingParityShard(t *testing.T) {
	backend := newMemoryShardBackend()
	st, err := NewStorage(backend, Options{Owner: "owner", DataShards: 4, ParityShards: 2})
	if err != nil {
		t.Fatalf("NewStorage returned error: %v", err)
	}

	if err := st.PutBlob(context.Background(), "blob-1", testBytes("resilient payload"), blob.PutOptions{}); err != nil {
		t.Fatalf("PutBlob returned error: %v", err)
	}

	backend.deleteOneForBlob("blob-1")
	var out testOutput
	if err := st.GetBlob(context.Background(), "blob-1", 0, -1, &out); err != nil {
		t.Fatalf("GetBlob returned error after deleting one shard: %v", err)
	}
	if string(out.bytes) != "resilient payload" {
		t.Fatalf("output = %q", out.bytes)
	}
}

func TestStoragePersistsManifestIndex(t *testing.T) {
	backend := newMemoryShardBackend()
	manifestPath := t.TempDir() + "/manifests.json"

	st, err := NewStorage(backend, Options{
		Owner:        "owner",
		DataShards:   2,
		ParityShards: 1,
		ManifestPath: manifestPath,
	})
	if err != nil {
		t.Fatalf("NewStorage returned error: %v", err)
	}
	if err := st.PutBlob(context.Background(), "blob-1", testBytes("persistent manifest"), blob.PutOptions{}); err != nil {
		t.Fatalf("PutBlob returned error: %v", err)
	}

	reopened, err := NewStorage(backend, Options{
		Owner:        "owner",
		DataShards:   2,
		ParityShards: 1,
		ManifestPath: manifestPath,
	})
	if err != nil {
		t.Fatalf("reopen NewStorage returned error: %v", err)
	}

	var out testOutput
	if err := reopened.GetBlob(context.Background(), "blob-1", 0, -1, &out); err != nil {
		t.Fatalf("GetBlob returned error after reopen: %v", err)
	}
	if string(out.bytes) != "persistent manifest" {
		t.Fatalf("output = %q, want persistent manifest", out.bytes)
	}
}

func newTestStorage(t *testing.T) *Storage {
	t.Helper()

	st, err := NewStorage(newMemoryShardBackend(), Options{Owner: "owner", DataShards: 4, ParityShards: 2})
	if err != nil {
		t.Fatalf("NewStorage returned error: %v", err)
	}
	return st
}

type memoryShardBackend struct {
	mu     sync.Mutex
	shards map[blobserver.ShardRef][]byte
}

func newMemoryShardBackend() *memoryShardBackend {
	return &memoryShardBackend{shards: map[blobserver.ShardRef][]byte{}}
}

func (b *memoryShardBackend) PutShard(ctx context.Context, req blobserver.PutShardRequest) error {
	if req.Hash != blobserver.Hash(req.Data) {
		return errors.New("hash mismatch")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.shards[req.Ref] = append([]byte(nil), req.Data...)
	return nil
}

func (b *memoryShardBackend) GetShard(ctx context.Context, req blobserver.GetShardRequest) (blobserver.GetShardResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.shards[req.Ref]
	if !ok {
		return blobserver.GetShardResponse{}, blobserver.ErrShardNotFound
	}
	data = append([]byte(nil), data...)
	return blobserver.GetShardResponse{Ref: req.Ref, Data: data, Hash: blobserver.Hash(data)}, nil
}

func (b *memoryShardBackend) DeleteShard(ctx context.Context, req blobserver.DeleteShardRequest) (blobserver.DeleteShardResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.shards[req.Ref]
	delete(b.shards, req.Ref)
	return blobserver.DeleteShardResponse{Deleted: ok}, nil
}

func (b *memoryShardBackend) deleteOneForBlob(blobID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var refs []blobserver.ShardRef
	for ref := range b.shards {
		if ref.BlobID == blobID {
			refs = append(refs, ref)
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Index < refs[j].Index })
	if len(refs) > 0 {
		delete(b.shards, refs[0])
	}
}

type testBytes []byte

func (b testBytes) Length() int {
	return len(b)
}

func (b testBytes) Reader() io.ReadSeekCloser {
	return nopReadSeekCloser{Reader: bytes.NewReader(b)}
}

func (b testBytes) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(b)
	return int64(n), err
}

type nopReadSeekCloser struct {
	*bytes.Reader
}

func (nopReadSeekCloser) Close() error {
	return nil
}

type testOutput struct {
	bytes []byte
}

func (o *testOutput) Reset() {
	o.bytes = nil
}

func (o *testOutput) Length() int {
	return len(o.bytes)
}

func (o *testOutput) Write(p []byte) (int, error) {
	o.bytes = append(o.bytes, p...)
	return len(p), nil
}
