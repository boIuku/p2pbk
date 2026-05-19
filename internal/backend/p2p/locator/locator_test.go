package locator

import (
	"context"
	"testing"

	"p2pbk/internal/peer/blobserver"
)

func TestMemoryLocatorRoundTrip(t *testing.T) {
	loc := NewMemory()
	locations := []Location{
		{Peer: "peer-b", Ref: blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 1}},
		{Peer: "peer-a", Ref: blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 0}},
	}

	if err := loc.Put(context.Background(), "blob", locations); err != nil {
		t.Fatalf("Put returned error: %v", err)
	}

	got, err := loc.Get(context.Background(), "blob")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if len(got) != 2 || got[0].Ref.Index != 0 || got[1].Ref.Index != 1 {
		t.Fatalf("locations not sorted by shard index: %+v", got)
	}

	if err := loc.Delete(context.Background(), "blob"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	got, err = loc.Get(context.Background(), "blob")
	if err != nil {
		t.Fatalf("Get after delete returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("locations after delete = %+v, want empty", got)
	}
}
