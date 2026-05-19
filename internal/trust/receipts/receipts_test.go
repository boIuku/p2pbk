package receipts

import (
	"path/filepath"
	"testing"
	"time"

	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/identity"
)

func TestIssueVerifyReceipt(t *testing.T) {
	id := newIdentity(t)
	ref := blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 1}
	r, err := Issue(id, ref, "hash", "", time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	if err := Verify(id.PublicKey(), r); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if r.StoredBy != id.Fingerprint() {
		t.Fatalf("stored_by = %q, want identity fingerprint", r.StoredBy)
	}
}

func TestVerifyRejectsTamperedReceipt(t *testing.T) {
	id := newIdentity(t)
	ref := blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 1}
	r, err := Issue(id, ref, "hash", "", time.Unix(10, 0))
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	r.Hash = "other"
	if err := Verify(id.PublicKey(), r); err == nil {
		t.Fatal("Verify returned nil error for tampered receipt")
	}
}

func newIdentity(t *testing.T) *identity.Identity {
	t.Helper()

	id, err := identity.Create(filepath.Join(t.TempDir(), "identity.age"), "passphrase")
	if err != nil {
		t.Fatalf("identity Create returned error: %v", err)
	}
	return id
}
