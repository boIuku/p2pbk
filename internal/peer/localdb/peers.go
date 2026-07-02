package localdb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

type PeerRecord struct {
	ID             string    `json:"id"`
	Addrs          []string  `json:"addrs"`
	Source         string    `json:"source"`
	FirstSeen      time.Time `json:"first_seen"`
	LastSeen       time.Time `json:"last_seen"`
	LastSuccess    time.Time `json:"last_success,omitempty"`
	LastFailure    time.Time `json:"last_failure,omitempty"`
	StoredShards   int       `json:"stored_shards"`
	AuditSuccesses int       `json:"audit_successes"`
	AuditFailures  int       `json:"audit_failures"`
	Failures       int       `json:"failures"`
}

func (db *DB) UpsertPeer(record PeerRecord) error {
	if record.ID == "" {
		return errors.New("peer id is required")
	}
	now := time.Now().UTC()
	record.Addrs = uniqueStrings(record.Addrs)
	if record.Source == "" {
		record.Source = "observed"
	}
	if record.FirstSeen.IsZero() {
		record.FirstSeen = now
	}
	if record.LastSeen.IsZero() {
		record.LastSeen = now
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer rollback(tx)

	existing, err := peerRecordInTx(tx, record.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		record.FirstSeen = existing.FirstSeen
		record.Addrs = uniqueStrings(append(existing.Addrs, record.Addrs...))
		if record.LastSuccess.IsZero() {
			record.LastSuccess = existing.LastSuccess
		}
		if record.Failures == 0 {
			record.Failures = existing.Failures
		}
		if record.LastFailure.IsZero() {
			record.LastFailure = existing.LastFailure
		}
		if record.StoredShards == 0 {
			record.StoredShards = existing.StoredShards
		}
		if record.AuditSuccesses == 0 {
			record.AuditSuccesses = existing.AuditSuccesses
		}
		if record.AuditFailures == 0 {
			record.AuditFailures = existing.AuditFailures
		}
	}
	if err := putPeerInTx(tx, record); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) ListPeers() ([]PeerRecord, error) {
	rows, err := db.conn.Query(`SELECT data FROM peers ORDER BY last_seen DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PeerRecord
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record PeerRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, fmt.Errorf("unmarshal peer record: %w", err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out, nil
}

func (db *DB) RecordPeerFailure(peerID string, at time.Time) error {
	if peerID == "" {
		return errors.New("peer id is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return db.updatePeer(peerID, func(record *PeerRecord) {
		record.LastFailure = at.UTC()
		record.Failures++
	})
}

func (db *DB) updatePeer(peerID string, update func(*PeerRecord)) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err := upsertPeerInTx(tx, peerID, update); err != nil {
		return err
	}
	return tx.Commit()
}

func peerRecordInTx(tx *sql.Tx, peerID string) (*PeerRecord, error) {
	var data []byte
	err := tx.QueryRow(`SELECT data FROM peers WHERE id = ?`, peerID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record PeerRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("unmarshal peer record: %w", err)
	}
	return &record, nil
}

func putPeerInTx(tx *sql.Tx, record PeerRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal peer record: %w", err)
	}
	_, err = tx.Exec(
		`INSERT INTO peers(id, last_seen, data) VALUES(?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET last_seen = excluded.last_seen, data = excluded.data`,
		record.ID,
		timeKey(record.LastSeen),
		data,
	)
	return err
}

func upsertPeerInTx(tx *sql.Tx, peerID string, update func(*PeerRecord)) error {
	record, err := peerRecordInTx(tx, peerID)
	if err != nil {
		return err
	}
	if record == nil {
		now := time.Now().UTC()
		record = &PeerRecord{ID: peerID, Source: "observed", FirstSeen: now, LastSeen: now}
	}
	update(record)
	return putPeerInTx(tx, *record)
}

func rollback(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
