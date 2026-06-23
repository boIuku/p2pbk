package localdb

import (
	"path/filepath"
	"testing"
	"time"

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

func TestOpenReusesHandleInProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local.db")
	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open returned error: %v", err)
	}
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open returned error: %v", err)
	}
	if err := db1.PutSetting("shared", []byte("ok")); err != nil {
		t.Fatalf("PutSetting returned error: %v", err)
	}
	got, err := db2.GetSetting("shared")
	if err != nil {
		t.Fatalf("GetSetting returned error: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("setting = %q, want ok", got)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("first Close returned error: %v", err)
	}
	if err := db2.PutSetting("after_first_close", []byte("ok")); err != nil {
		t.Fatalf("PutSetting after first close returned error: %v", err)
	}
	if err := db2.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
}

func TestPeerRecordRoundTripAndMerge(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	first := time.Unix(1, 0)
	if err := db.UpsertPeer(PeerRecord{
		ID:        "peer-a",
		Addrs:     []string{"/ip4/127.0.0.1/tcp/4001/p2p/peer-a"},
		Source:    "config",
		FirstSeen: first,
		LastSeen:  first,
	}); err != nil {
		t.Fatalf("UpsertPeer returned error: %v", err)
	}
	if err := db.UpsertPeer(PeerRecord{
		ID:       "peer-a",
		Addrs:    []string{"/dns4/node1/tcp/4001/p2p/peer-a"},
		Source:   "mdns",
		LastSeen: time.Unix(2, 0),
	}); err != nil {
		t.Fatalf("UpsertPeer merge returned error: %v", err)
	}

	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers returned error: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("peer count = %d, want 1", len(peers))
	}
	if peers[0].FirstSeen != first {
		t.Fatalf("first seen = %s, want %s", peers[0].FirstSeen, first)
	}
	if len(peers[0].Addrs) != 2 {
		t.Fatalf("addrs = %v, want two merged addrs", peers[0].Addrs)
	}
}

func TestRecordPeerFailure(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	at := time.Unix(3, 0)
	if err := db.RecordPeerFailure("peer-a", at); err != nil {
		t.Fatalf("RecordPeerFailure returned error: %v", err)
	}
	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers returned error: %v", err)
	}
	if len(peers) != 1 || peers[0].Failures != 1 || !peers[0].LastFailure.Equal(at) {
		t.Fatalf("peers = %+v", peers)
	}
}
