package localdb

import (
	"path/filepath"
	"testing"
	"time"

	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/receipts"
)

func TestShardReceiptAndAuditHistory(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	ref := blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 1}
	if err := db.StoreShardReceipt(ShardReceiptRecord{
		PeerID: "peer-a",
		Receipt: receipts.Receipt{
			Ref:       receipts.ShardRef{Owner: ref.Owner, BlobID: ref.BlobID, Index: ref.Index},
			Hash:      "hash",
			StoredBy:  "peer-a",
			IssuedAt:  time.Unix(1, 0),
			Signature: "sig",
		},
		StoredAt: time.Unix(1, 0),
	}); err != nil {
		t.Fatalf("StoreShardReceipt returned error: %v", err)
	}

	receiptList, err := db.ListShardReceipts()
	if err != nil {
		t.Fatalf("ListShardReceipts returned error: %v", err)
	}
	if len(receiptList) != 1 || receiptList[0].PeerID != "peer-a" || receiptList[0].Receipt.Ref.BlobID != ref.BlobID {
		t.Fatalf("receipt list = %+v", receiptList)
	}
	if err := db.StoreShardReceipt(ShardReceiptRecord{
		PeerID:   "peer-a",
		Receipt:  receiptList[0].Receipt,
		StoredAt: time.Unix(4, 0),
	}); err != nil {
		t.Fatalf("replaying receipt returned error: %v", err)
	}
	peers, err := db.ListPeers()
	if err != nil || len(peers) != 1 || peers[0].StoredShards != 1 {
		t.Fatalf("replayed receipt double-counted: peers=%+v err=%v", peers, err)
	}

	if err := db.RecordAudit(AuditRecord{PeerID: "peer-a", Ref: ref, Success: true, CheckedAt: time.Unix(2, 0)}); err != nil {
		t.Fatalf("RecordAudit success returned error: %v", err)
	}
	if err := db.RecordAudit(AuditRecord{PeerID: "peer-a", Ref: ref, Success: false, Error: "offline", CheckedAt: time.Unix(3, 0)}); err != nil {
		t.Fatalf("RecordAudit failure returned error: %v", err)
	}

	history, err := db.ListAuditHistory("peer-a")
	if err != nil {
		t.Fatalf("ListAuditHistory returned error: %v", err)
	}
	if len(history) != 2 || history[0].Error != "offline" {
		t.Fatalf("history = %+v", history)
	}

	stats, err := db.ListPeerStats()
	if err != nil {
		t.Fatalf("ListPeerStats returned error: %v", err)
	}
	if len(stats) != 1 || stats[0].AuditSuccesses != 1 || stats[0].AuditFailures != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}
