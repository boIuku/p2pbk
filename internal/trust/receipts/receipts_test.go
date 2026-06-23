package receipts

import (
	"path/filepath"
	"testing"
	"time"

	"p2pbk/internal/trust/identity"
)

func TestIssueVerifyReceipt(t *testing.T) {
	id, err := identity.Create(filepath.Join(t.TempDir(), "identity.age"), "pass")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	ref := ShardRef{Owner: "owner", BlobID: "blob", Index: 1}
	receipt, err := Issue(id, ref, "hash", "peer", time.Unix(1, 0))
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}
	if err := Verify(id.PublicKey(), receipt); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}

func TestVerifyRejectsTamperedReceipt(t *testing.T) {
	id, err := identity.Create(filepath.Join(t.TempDir(), "identity.age"), "pass")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	receipt, err := Issue(id, ShardRef{Owner: "owner", BlobID: "blob", Index: 1}, "hash", "peer", time.Unix(1, 0))
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}
	receipt.Hash = "other"
	if err := Verify(id.PublicKey(), receipt); err == nil {
		t.Fatal("Verify returned nil error for tampered receipt")
	}
}
