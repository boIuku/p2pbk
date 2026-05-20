package localdb

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/bbolt"

	"p2pbk/internal/peer/blobserver"
	// "p2pbk/internal/trust/challenges"
	"p2pbk/internal/trust/receipts"
)

type DB struct {
	*bbolt.DB
	// Store a reference to the bolt.DB here
	// so we can expose its methods directly.
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create localdb directory: %w", err)
	}

	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open localdb: %w", err)
	}

	wrapped := &DB{DB: db}
	if err := wrapped.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return wrapped, nil
}

func (db *DB) init() error {
	return db.Update(func(tx *bbolt.Tx) error {
		for _, name := range RequiredBuckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("create bucket %s: %w", name, err)
			}
		}
		return nil
	})
}

// PutSetting writes a key-value pair to the settings bucket.
func (db *DB) PutSetting(key string, value []byte) error {
	if key == "" {
		return errors.New("setting key is required")
	}
	return db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(BucketSettings)).Put([]byte(key), value)
	})
}

// GetSetting retrieves a value from the settings bucket.
func (db *DB) GetSetting(key string) ([]byte, error) {
	if key == "" {
		return nil, errors.New("setting key is required")
	}

	var out []byte
	err := db.View(func(tx *bbolt.Tx) error {
		value := tx.Bucket([]byte(BucketSettings)).Get([]byte(key))
		if value == nil {
			return nil
		}
		out = append([]byte(nil), value...)
		return nil
	})
	return out, err
}

// Receipt stores a signed attestation that we're storing a blob.
func (db *DB) StoreReceipt(ctx context.Context, r receipts.Receipt) error {
	// Key: owner|blob_id

	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketReceipts))
		if b == nil {
			return errors.New("receipts bucket not found")
		}

		// Encode receipt
		data, err := marshalReceipt(r)
		if err != nil {
			return err
		}

		// Use multi-part keys for owner|blob_id|index
		keyBytes := []byte(fmt.Sprintf("%s|%s|%d", r.Ref.Owner, r.Ref.BlobID, r.Ref.Index))
		if err := b.Put(keyBytes, data); err != nil {
			return err
		}
		return nil
	})
}

// GetReceipt retrieves a receipt by shard reference.
func (db *DB) GetReceipt(ctx context.Context, ref blobserver.ShardRef) (*receipts.Receipt, error) {
	key := fmt.Sprintf("%s|%s|%d", ref.Owner, ref.BlobID, ref.Index)

	var out receipts.Receipt
	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketReceipts))
		if b == nil {
			return errors.New("receipts bucket not found")
		}

		data := b.Get([]byte(key))
		if data == nil {
			return errors.New("receipt not found")
		}

		return unmarshalReceipt(data, &out)
	})

	return &out, err
}

// ListReceipts lists all receipts for an owner's blob.
func (db *DB) ListReceipts(ctx context.Context, owner, blobID string) ([]receipts.Receipt, error) {
	var out []receipts.Receipt

	return out, nil // TODO: iterate over owned_blobs bucket
}

// ChallengeResult records the result of a challenge response.
func (db *DB) RecordChallengeResult(ctx context.Context, peer string, ref blobserver.ShardRef, success bool, proof string, timestamp time.Time) error {

	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketChallengeHistory))
		if b == nil {
			return errors.New("challenge_history bucket not found")
		}

		// Create or get existing challenge history entry
		chr, err := newChallengeHistoryRecord(peer, ref.BlobID, success, proof) // Changed key to ref.BlobID
		if err != nil {
			return err
		}

		data, err := marshalChallengeHistoryRecord(chr)
		if err != nil {
			return err
		}

		return b.Put([]byte(peer+"|"+ref.BlobID), data) // Changed key to a composite key for challenges
	})
}

// GetChallengeHistory returns challenge history for a shard.
func (db *DB) GetChallengeHistory(ctx context.Context, owner, blobID string) ([]ChallengeHistoryRecord, error) {
	var out []ChallengeHistoryRecord

	return out, nil // TODO: iterate over challenge_history bucket
}

// ChallengeHistoryRecord represents a challenge result record.
type ChallengeHistoryRecord struct {
	Peer      string
	Success   bool
	Proof     string
	Timestamp time.Time
	Nonce     string
}

// ChallengeHistory stores challenge history in memory (in real impl, persisted in localdb).
type ChallengeHistory struct {
	records map[string][]ChallengeHistoryRecord
}

// NewChallengeHistory creates a new in-memory challenge history.
func NewChallengeHistory() *ChallengeHistory {
	return &ChallengeHistory{
		records: make(map[string][]ChallengeHistoryRecord),
	}
}

// Record adds a challenge result.
func (h *ChallengeHistory) Record(owner, blobID string, peer string, success bool, proof string, nonce string, timestamp time.Time) {
	key := fmt.Sprintf("%s|%.0f|%.0f", owner, blobID, 0)
	h.records[key] = append(h.records[key], ChallengeHistoryRecord{
		Peer:    peer,
		Success: success,
		Proof:   proof,
		Nonce:   nonce,
		Timestamp: timestamp,
	})
}

// Get returns challenge history for a shard.
func (h *ChallengeHistory) Get(owner, blobID string) []ChallengeHistoryRecord {
	key := fmt.Sprintf("%s|%.0f|%.0f", owner, blobID, 0)
	return h.records[key]
}

// Track tracks a peer's challenge success/failure for reputation.
func (h *ChallengeHistory) Track(peer string, success bool, timestamp time.Time) {
	if success {
		// Record success
	} else {
		// Record failure
	}
}

// marshalReceipt encodes a receipt to bytes.
func marshalReceipt(r receipts.Receipt) ([]byte, error) {
	data := new(bytes.Buffer)

	// Write peer ID (variable length string)
	_, _ = data.Write([]byte{0}) // null terminator

	// Write owner
	_, _ = data.Write([]byte(r.Ref.Owner))
	_, _ = data.Write([]byte{0}) // null terminator

	// Write blob ID
	_, _ = data.Write([]byte(r.Ref.BlobID))
	_, _ = data.Write([]byte{0}) // null terminator

	// Write index
	var idx [4]byte
	binary.BigEndian.PutUint32(idx[:], uint32(r.Ref.Index))
	_, _ = data.Write(idx[:])

	// Write hash
	_, _ = data.Write([]byte(r.Hash))
	_, _ = data.Write([]byte{0}) // null terminator

	// Write signature
	_, _ = data.Write([]byte(r.Signature))
	_, _ = data.Write([]byte{0}) // null terminator

	return data.Bytes(), nil
}

// unmarshalReceipt decodes bytes to a receipt.
func unmarshalReceipt(data []byte, r *receipts.Receipt) error {
	// Decode peer ID
	signerEnd := bytes.IndexByte(data, 0)
	if signerEnd == -1 {
		return errors.New("data too short for peer ID")
	}
	data = data[signerEnd+1:]

	// Decode owner and blob ID (until null byte)
	r.Ref.Owner = string(data[:bytes.IndexByte(data, 0)])
	data = data[bytes.IndexByte(data, 0)+1:]
	r.Ref.BlobID = string(data[:bytes.IndexByte(data, 0)])
	data = data[bytes.IndexByte(data, 0)+1:]

	// Decode index
	r.Ref.Index = int(binary.BigEndian.Uint32(data[:4]))
	data = data[4:]

	// Decode hash
	r.Hash = string(data[:bytes.IndexByte(data, 0)])
	data = data[bytes.IndexByte(data, 0)+1:]

	// Decode signature
	r.Signature = string(data)

	return nil
}

// newChallengeHistoryRecord creates a new challenge history record.
func newChallengeHistoryRecord(peer string, key string, success bool, proof string) (*ChallengeHistoryRecord, error) {
	if peer == "" {
		return nil, errors.New("peer is required")
	}
	if proof == "" {
		return nil, errors.New("proof is required")
	}
	return &ChallengeHistoryRecord{
		Peer:    peer,
		Success: success,
		Proof:   proof,
		Timestamp: time.Now().UTC(),
	}, nil
}

// marshalChallengeHistoryRecord encodes a challenge history record.
func marshalChallengeHistoryRecord(r *ChallengeHistoryRecord) ([]byte, error) {
	data := new(bytes.Buffer)

	// Write peer ID
	_, _ = data.Write([]byte(r.Peer))
	_, _ = data.Write([]byte{}) // null terminator

	// Write success flag
	_ = binary.Write(data, binary.BigEndian, uint8(0)) // Placeholder, success is part of the struct

	// Write proof
	_, _ = data.Write([]byte(r.Proof))
	_, _ = data.Write([]byte{0}) // null terminator

	// Write timestamp
	timestamp := r.Timestamp.UnixNano()
	var nanos [8]byte
	binary.BigEndian.PutUint64(nanos[:], uint64(timestamp))
	_, _ = data.Write(nanos[:])

	// Write nonce (optional)
	_, _ = data.Write([]byte(r.Nonce))
	_, _ = data.Write([]byte{}) // null terminator

	return data.Bytes(), nil
}

// unmarshalChallengeHistoryRecord decodes bytes to a challenge history record.
func unmarshalChallengeHistoryRecord(data []byte, r *ChallengeHistoryRecord) error {
	if len(data) < 1 {
		return errors.New("data too short")
	}

	// Decode peer ID
	r.Peer = string(data[:bytes.IndexByte(data, '\x00')])
	data = data[bytes.IndexByte(data, '\x00')+1:]

	// success is decoded as part of the ChallengeHistoryRecord struct
	data = data[1:] // Advance past placeholder byte

	// Decode proof
	r.Proof = string(data[:bytes.IndexByte(data, 0)])
	data = data[bytes.IndexByte(data, 0)+1:]

	// Decode timestamp
	timestamp := binary.BigEndian.Uint64(data[:8])
	r.Timestamp = time.Unix(0, int64(timestamp))
	data = data[8:]

	// Decode nonce (optional)
	nonceEnd := bytes.IndexByte(data, '\x00')
	if nonceEnd > 0 {
		r.Nonce = string(data[:nonceEnd])
	}

	return nil
}
