package locator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"

	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/identity"
)

func TestDHTLocatorUsesSignedRecords(t *testing.T) {
	id, err := identity.Create(filepath.Join(t.TempDir(), "identity.age"), "passphrase")
	if err != nil {
		t.Fatalf("identity Create returned error: %v", err)
	}
	router := memoryValueRouter{values: map[string][]byte{}}
	loc, err := NewDHT(&router, id, nil)
	if err != nil {
		t.Fatalf("NewDHT returned error: %v", err)
	}
	peerID, err := peer.Decode("12D3KooWQxLxm6iMN5r6tcbpPE4E7YXE5E1BZ1LmYy68KQQZkQnT")
	if err != nil {
		t.Fatalf("peer Decode returned error: %v", err)
	}
	ref := blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 0}
	if err := loc.Put(context.Background(), "blob", []Location{{Peer: peerID, Ref: ref}}); err != nil {
		t.Fatalf("Put returned error: %v", err)
	}
	got, err := loc.Get(context.Background(), "blob")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if len(got) != 1 || got[0].Peer != peerID || got[0].Ref != ref {
		t.Fatalf("locations = %+v", got)
	}
}

type memoryValueRouter struct {
	values map[string][]byte
}

func (m *memoryValueRouter) PutValue(ctx context.Context, key string, value []byte) error {
	m.values[key] = append([]byte(nil), value...)
	return nil
}

func (m *memoryValueRouter) GetValue(ctx context.Context, key string) ([]byte, error) {
	return append([]byte(nil), m.values[key]...), nil
}
