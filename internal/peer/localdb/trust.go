package localdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.etcd.io/bbolt"

	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/receipts"
)

type ShardReceiptRecord struct {
	PeerID   string           `json:"peer_id"`
	Receipt  receipts.Receipt `json:"receipt"`
	StoredAt time.Time        `json:"stored_at"`
}

type AuditRecord struct {
	PeerID    string              `json:"peer_id"`
	Ref       blobserver.ShardRef `json:"ref"`
	Success   bool                `json:"success"`
	Error     string              `json:"error,omitempty"`
	CheckedAt time.Time           `json:"checked_at"`
}

type PeerStats struct {
	PeerRecord
	Score  float64 `json:"score"`
	Status string  `json:"status"`
}

func (db *DB) StoreShardReceipt(record ShardReceiptRecord) error {
	if record.PeerID == "" {
		return errors.New("peer id is required")
	}
	if err := record.Receipt.Ref.Validate(); err != nil {
		return err
	}
	if record.Receipt.Hash == "" {
		return errors.New("receipt hash is required")
	}
	if record.StoredAt.IsZero() {
		record.StoredAt = time.Now().UTC()
	}
	record.StoredAt = record.StoredAt.UTC()

	return db.Update(func(tx *bbolt.Tx) error {
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal shard receipt: %w", err)
		}
		if err := tx.Bucket([]byte(BucketShardReceipts)).Put([]byte(receiptKey(record.PeerID, record.Receipt.Ref)), data); err != nil {
			return err
		}
		return upsertPeerInTx(tx, record.PeerID, func(peer *PeerRecord) {
			peer.LastSeen = record.StoredAt
			peer.StoredShards++
		})
	})
}

func (db *DB) ListShardReceipts() ([]ShardReceiptRecord, error) {
	var out []ShardReceiptRecord
	err := db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(BucketShardReceipts)).ForEach(func(_, v []byte) error {
			var record ShardReceiptRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return fmt.Errorf("unmarshal shard receipt: %w", err)
			}
			out = append(out, record)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].StoredAt.Equal(out[j].StoredAt) {
			return out[i].PeerID < out[j].PeerID
		}
		return out[i].StoredAt.After(out[j].StoredAt)
	})
	return out, err
}

func (db *DB) RecordAudit(record AuditRecord) error {
	if record.PeerID == "" {
		return errors.New("peer id is required")
	}
	if err := record.Ref.Validate(); err != nil {
		return err
	}
	if record.CheckedAt.IsZero() {
		record.CheckedAt = time.Now().UTC()
	}
	record.CheckedAt = record.CheckedAt.UTC()

	return db.Update(func(tx *bbolt.Tx) error {
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal audit record: %w", err)
		}
		key := fmt.Sprintf("%s|%s|%d|%d", record.PeerID, record.Ref.BlobID, record.Ref.Index, record.CheckedAt.UnixNano())
		if err := tx.Bucket([]byte(BucketAuditHistory)).Put([]byte(key), data); err != nil {
			return err
		}
		return upsertPeerInTx(tx, record.PeerID, func(peer *PeerRecord) {
			peer.LastSeen = record.CheckedAt
			if record.Success {
				peer.LastSuccess = record.CheckedAt
				peer.AuditSuccesses++
				return
			}
			peer.LastFailure = record.CheckedAt
			peer.AuditFailures++
			peer.Failures++
		})
	})
}

func (db *DB) ListAuditHistory(peerID string) ([]AuditRecord, error) {
	var out []AuditRecord
	err := db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(BucketAuditHistory)).ForEach(func(_, v []byte) error {
			var record AuditRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return fmt.Errorf("unmarshal audit record: %w", err)
			}
			if peerID == "" || record.PeerID == peerID {
				out = append(out, record)
			}
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		return out[i].CheckedAt.After(out[j].CheckedAt)
	})
	return out, err
}

func (db *DB) ListPeerStats() ([]PeerStats, error) {
	peers, err := db.ListPeers()
	if err != nil {
		return nil, err
	}
	out := make([]PeerStats, 0, len(peers))
	for _, peer := range peers {
		score := ScorePeer(peer)
		out = append(out, PeerStats{
			PeerRecord: peer,
			Score:      score,
			Status:     PeerStatus(score, peer),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	return out, nil
}

func ScorePeer(peer PeerRecord) float64 {
	successes := float64(peer.AuditSuccesses)
	failures := float64(peer.AuditFailures + peer.Failures)
	total := successes + failures
	if total == 0 {
		return 0.5
	}
	score := successes / total
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func PeerStatus(score float64, peer PeerRecord) string {
	if peer.AuditFailures+peer.Failures >= 5 && score < 0.35 {
		return "blocked"
	}
	if score < 0.6 || peer.AuditFailures > peer.AuditSuccesses {
		return "suspect"
	}
	if peer.AuditSuccesses == 0 {
		return "new"
	}
	return "healthy"
}

func upsertPeerInTx(tx *bbolt.Tx, peerID string, update func(*PeerRecord)) error {
	b := tx.Bucket([]byte(BucketPeers))
	record, err := peerRecordFromBucket(b, peerID)
	if err != nil {
		return err
	}
	if record == nil {
		now := time.Now().UTC()
		record = &PeerRecord{ID: peerID, Source: "observed", FirstSeen: now, LastSeen: now}
	}
	update(record)
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal peer record: %w", err)
	}
	return b.Put([]byte(peerID), data)
}

func receiptKey(peerID string, ref receipts.ShardRef) string {
	return fmt.Sprintf("%s|%s|%d", peerID, ref.BlobID, ref.Index)
}
