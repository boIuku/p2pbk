package locator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"

	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/identity"
)

func TestPersistentLocatorRoundTripAndVerify(t *testing.T) {
	id := newTestIdentity(t)
	loc, err := OpenPersistent(filepath.Join(t.TempDir(), "locator.db"), id, nil)
	if err != nil {
		t.Fatalf("OpenPersistent returned error: %v", err)
	}
	defer loc.Close()

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

func newTestIdentity(t *testing.T) *identity.Identity {
	t.Helper()
	id, err := identity.Create(filepath.Join(t.TempDir(), "identity.age"), "passphrase")
	if err != nil {
		t.Fatalf("identity Create returned error: %v", err)
	}
	return id
}
