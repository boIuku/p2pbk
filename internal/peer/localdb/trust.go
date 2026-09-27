package localdb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

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

	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer rollback(tx)

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal shard receipt: %w", err)
	}
	key := receiptKey(record.PeerID, record.Receipt.Ref)
	var existing int
	existsErr := tx.QueryRow(`SELECT 1 FROM shard_receipts WHERE key = ?`, key).Scan(&existing)
	if existsErr != nil && !errors.Is(existsErr, sql.ErrNoRows) {
		return existsErr
	}
	if _, err := tx.Exec(
		`INSERT INTO shard_receipts(key, peer_id, stored_at, data) VALUES(?, ?, ?, ?)
			 ON CONFLICT(key) DO UPDATE SET peer_id = excluded.peer_id, stored_at = excluded.stored_at, data = excluded.data`,
		key,
		record.PeerID,
		timeKey(record.StoredAt),
		data,
	); err != nil {
		return err
	}
	if err := upsertPeerInTx(tx, record.PeerID, func(peer *PeerRecord) {
		peer.LastSeen = record.StoredAt
		if existsErr != nil {
			peer.StoredShards++
		}
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) ListShardReceipts() ([]ShardReceiptRecord, error) {
	rows, err := db.conn.Query(`SELECT data FROM shard_receipts ORDER BY stored_at DESC, peer_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ShardReceiptRecord
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record ShardReceiptRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, fmt.Errorf("unmarshal shard receipt: %w", err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StoredAt.Equal(out[j].StoredAt) {
			return out[i].PeerID < out[j].PeerID
		}
		return out[i].StoredAt.After(out[j].StoredAt)
	})
	return out, nil
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

	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer rollback(tx)

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal audit record: %w", err)
	}
	key := fmt.Sprintf("%s|%s|%d|%d", record.PeerID, record.Ref.BlobID, record.Ref.Index, record.CheckedAt.UnixNano())
	if _, err := tx.Exec(
		`INSERT INTO audit_history(key, peer_id, checked_at, data) VALUES(?, ?, ?, ?)`,
		key,
		record.PeerID,
		timeKey(record.CheckedAt),
		data,
	); err != nil {
		return err
	}
	if err := upsertPeerInTx(tx, record.PeerID, func(peer *PeerRecord) {
		peer.LastSeen = record.CheckedAt
		if record.Success {
			peer.LastSuccess = record.CheckedAt
			peer.AuditSuccesses++
			return
		}
		peer.LastFailure = record.CheckedAt
		peer.AuditFailures++
		peer.Failures++
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) ListAuditHistory(peerID string) ([]AuditRecord, error) {
	query := `SELECT data FROM audit_history ORDER BY checked_at DESC`
	args := []any(nil)
	if peerID != "" {
		query = `SELECT data FROM audit_history WHERE peer_id = ? ORDER BY checked_at DESC`
		args = append(args, peerID)
	}
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditRecord
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record AuditRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, fmt.Errorf("unmarshal audit record: %w", err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CheckedAt.After(out[j].CheckedAt)
	})
	return out, nil
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

func receiptKey(peerID string, ref receipts.ShardRef) string {
	return fmt.Sprintf("%s|%s|%d", peerID, ref.BlobID, ref.Index)
}
