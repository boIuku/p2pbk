package localdb

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Approval records an explicit storage-owner admission decision. Discovery and
// connectivity never create an approval. A revoked owner may still read and
// delete its already stored shards; only new writes are blocked.
type Approval struct {
	PeerID        string    `json:"peer_id"`
	FailureDomain string    `json:"failure_domain,omitempty"`
	ApprovedAt    time.Time `json:"approved_at"`
	RevokedAt     time.Time `json:"revoked_at,omitempty"`
	Note          string    `json:"note,omitempty"`
}

func (db *DB) ApprovePeer(peerID, failureDomain, note string, at time.Time) error {
	if peerID == "" {
		return errors.New("peer id is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	record := Approval{
		PeerID:        peerID,
		FailureDomain: failureDomain,
		ApprovedAt:    at.UTC(),
		Note:          note,
	}
	data, err := marshalApproval(record)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec(`
		INSERT INTO approvals(peer_id, approved, updated_at, data) VALUES(?, 1, ?, ?)
		ON CONFLICT(peer_id) DO UPDATE SET approved = 1, updated_at = excluded.updated_at, data = excluded.data`,
		peerID, timeKey(record.ApprovedAt), data)
	return err
}

func (db *DB) RevokePeer(peerID, note string, at time.Time) error {
	if peerID == "" {
		return errors.New("peer id is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	previous, err := db.GetApproval(peerID)
	if err != nil {
		return err
	}
	if previous == nil {
		previous = &Approval{PeerID: peerID, ApprovedAt: at.UTC()}
	}
	previous.RevokedAt = at.UTC()
	if note != "" {
		previous.Note = note
	}
	data, err := marshalApproval(*previous)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec(`
		INSERT INTO approvals(peer_id, approved, updated_at, data) VALUES(?, 0, ?, ?)
		ON CONFLICT(peer_id) DO UPDATE SET approved = 0, updated_at = excluded.updated_at, data = excluded.data`,
		peerID, timeKey(at.UTC()), data)
	return err
}

func (db *DB) IsPeerApproved(peerID string) (bool, error) {
	if peerID == "" {
		return false, errors.New("peer id is required")
	}
	var approved int
	err := db.conn.QueryRow(`SELECT approved FROM approvals WHERE peer_id = ?`, peerID).Scan(&approved)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return approved == 1, nil
}

func (db *DB) GetApproval(peerID string) (*Approval, error) {
	if peerID == "" {
		return nil, errors.New("peer id is required")
	}
	var data []byte
	err := db.conn.QueryRow(`SELECT data FROM approvals WHERE peer_id = ?`, peerID).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var record Approval
	if err := unmarshalApproval(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (db *DB) ListApprovals() ([]Approval, error) {
	rows, err := db.conn.Query(`SELECT data FROM approvals ORDER BY peer_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Approval
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record Approval
		if err := unmarshalApproval(data, &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PeerID < out[j].PeerID })
	return out, nil
}

func marshalApproval(record Approval) ([]byte, error) {
	data, err := jsonMarshal(record)
	if err != nil {
		return nil, fmt.Errorf("marshal peer approval: %w", err)
	}
	return data, nil
}

func unmarshalApproval(data []byte, record *Approval) error {
	if err := jsonUnmarshal(data, record); err != nil {
		return fmt.Errorf("unmarshal peer approval: %w", err)
	}
	return nil
}
