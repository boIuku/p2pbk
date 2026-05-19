package localdb

import (
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
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
