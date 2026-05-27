package localdb

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/receipts"
)

func TestOpenCreatesRequiredBuckets(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	err = db.View(func(tx *bolt.Tx) error {
		for _, name := range RequiredBuckets {
			if tx.Bucket([]byte(name)) == nil {
				t.Fatalf("missing bucket %s", name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("View returned error: %v", err)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	if err := db.PutSetting("schema_version", []byte("1")); err != nil {
		t.Fatalf("PutSetting returned error: %v", err)
	}

	value, err := db.GetSetting("schema_version")
	if err != nil {
		t.Fatalf("GetSetting returned error: %v", err)
	}
	if string(value) != "1" {
		t.Fatalf("setting value = %q, want 1", string(value))
	}
}

func TestReceiptsRoundTripAndList(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	r := receipts.Receipt{
		Ref:       blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 1},
		Hash:      "hash",
		StoredBy:  "peer",
		IssuedAt:  time.Unix(1, 0),
		Signature: "sig",
	}
	if err := db.StoreReceipt(context.Background(), r); err != nil {
		t.Fatalf("StoreReceipt returned error: %v", err)
	}
	got, err := db.GetReceipt(context.Background(), r.Ref)
	if err != nil {
		t.Fatalf("GetReceipt returned error: %v", err)
	}
	if got.Hash != r.Hash || got.Ref != r.Ref {
		t.Fatalf("receipt = %+v, want %+v", got, r)
	}
	list, err := db.ListReceipts(context.Background(), "owner", "blob")
	if err != nil {
		t.Fatalf("ListReceipts returned error: %v", err)
	}
	if len(list) != 1 || list[0].Ref != r.Ref {
		t.Fatalf("list = %+v", list)
	}
}

func TestChallengeHistoryRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	ref := blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 1}
	at := time.Unix(2, 0)
	if err := db.RecordChallengeResult(context.Background(), "peer", ref, true, "proof", at); err != nil {
		t.Fatalf("RecordChallengeResult returned error: %v", err)
	}
	history, err := db.GetChallengeHistory(context.Background(), "owner", "blob")
	if err != nil {
		t.Fatalf("GetChallengeHistory returned error: %v", err)
	}
	if len(history) != 1 || history[0].Peer != "peer" || !history[0].Success {
		t.Fatalf("history = %+v", history)
	}
}
