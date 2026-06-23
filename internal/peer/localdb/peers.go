package localdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.etcd.io/bbolt"
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

	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketPeers))
		existing, err := peerRecordFromBucket(b, record.ID)
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
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal peer record: %w", err)
		}
		return b.Put([]byte(record.ID), data)
	})
}

func (db *DB) RecordPeerSuccess(peerID string, at time.Time) error {
	if peerID == "" {
		return errors.New("peer id is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketPeers))
		record, err := peerRecordFromBucket(b, peerID)
		if err != nil {
			return err
		}
		if record == nil {
			record = &PeerRecord{ID: peerID, Source: "observed", FirstSeen: at}
		}
		record.LastSeen = at.UTC()
		record.LastSuccess = at.UTC()
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal peer record: %w", err)
		}
		return b.Put([]byte(peerID), data)
	})
}

func (db *DB) IncrementStoredShards(peerID string, delta int) error {
	if peerID == "" {
		return errors.New("peer id is required")
	}
	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketPeers))
		record, err := peerRecordFromBucket(b, peerID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if record == nil {
			record = &PeerRecord{ID: peerID, Source: "observed", FirstSeen: now}
		}
		record.LastSeen = now
		record.StoredShards += delta
		if record.StoredShards < 0 {
			record.StoredShards = 0
		}
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal peer record: %w", err)
		}
		return b.Put([]byte(peerID), data)
	})
}

func (db *DB) ListPeers() ([]PeerRecord, error) {
	var out []PeerRecord
	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketPeers))
		return b.ForEach(func(_, v []byte) error {
			var record PeerRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return fmt.Errorf("unmarshal peer record: %w", err)
			}
			out = append(out, record)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out, err
}

func (db *DB) RecordPeerFailure(peerID string, at time.Time) error {
	if peerID == "" {
		return errors.New("peer id is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketPeers))
		record, err := peerRecordFromBucket(b, peerID)
		if err != nil {
			return err
		}
		if record == nil {
			record = &PeerRecord{ID: peerID, Source: "observed", FirstSeen: at}
		}
		record.LastFailure = at.UTC()
		record.Failures++
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal peer record: %w", err)
		}
		return b.Put([]byte(peerID), data)
	})
}

func peerRecordFromBucket(b *bbolt.Bucket, peerID string) (*PeerRecord, error) {
	data := b.Get([]byte(peerID))
	if data == nil {
		return nil, nil
	}
	var record PeerRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("unmarshal peer record: %w", err)
	}
	return &record, nil
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
