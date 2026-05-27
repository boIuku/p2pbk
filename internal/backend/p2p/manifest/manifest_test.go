package manifest

import (
	"context"
	"io"
	"testing"
)

func TestLocalManifestPutGetDelete(t *testing.T) {
	local, err := NewLocalManifest(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalManifest returned error: %v", err)
	}
	m, err := New([]byte("manifest-data"))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := local.Put(context.Background(), m); err != nil {
		t.Fatalf("Put returned error: %v", err)
	}
	got, err := local.Get(context.Background(), m.Hash())
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if string(got.Data()) != "manifest-data" {
		t.Fatalf("data = %q", got.Data())
	}
	if err := local.Delete(context.Background(), m.Hash()); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, err := local.Get(context.Background(), m.Hash()); err == nil {
		t.Fatal("Get returned nil error after delete")
	}
}

func TestReplicatorStoresLocalReplica(t *testing.T) {
	replicator, err := NewReplicator(t.TempDir())
	if err != nil {
		t.Fatalf("NewReplicator returned error: %v", err)
	}
	m, err := New([]byte("small critical blob"))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := replicator.Replicate(context.Background(), m, []interface{}{"peer-a", "peer-b"}); err != nil {
		t.Fatalf("Replicate returned error: %v", err)
	}
	got, err := replicator.Get(context.Background(), m.Hash())
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if string(got.Data()) != "small critical blob" {
		t.Fatalf("data = %q", got.Data())
	}
}

func TestReadRangeAndReader(t *testing.T) {
	m, err := New([]byte("abcdef"))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	part, err := m.ReadRange(2, 3)
	if err != nil {
		t.Fatalf("ReadRange returned error: %v", err)
	}
	if string(part) != "cde" {
		t.Fatalf("range = %q, want cde", part)
	}

	reader := NewReader([]byte("abc"))
	buf := make([]byte, 2)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("first Read returned error: %v", err)
	}
	if n != 2 || string(buf) != "ab" {
		t.Fatalf("first read n=%d buf=%q", n, buf)
	}
	n, err = reader.Read(buf)
	if err != io.EOF {
		t.Fatalf("second Read error = %v, want EOF", err)
	}
	if n != 1 || string(buf[:n]) != "c" {
		t.Fatalf("second read n=%d buf=%q", n, buf[:n])
	}
}
