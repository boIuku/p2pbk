package localdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.etcd.io/bbolt"

	"p2pbk/internal/peer/blobserver"
	// "p2pbk/internal/trust/challenges"
	"p2pbk/internal/trust/receipts"
)

type DB struct {
	*bbolt.DB
	path string
}

type sharedDB struct {
	db   *bbolt.DB
	refs int
}

var (
	openMu  sync.Mutex
	openDBs = map[string]*sharedDB{}
)

func Open(path string) (*DB, error) {
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve localdb path: %w", err)
	}
	cleanPath = filepath.Clean(cleanPath)

	openMu.Lock()
	if existing := openDBs[cleanPath]; existing != nil {
		existing.refs++
		db := existing.db
		openMu.Unlock()
		return &DB{DB: db, path: cleanPath}, nil
	}
	openMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o700); err != nil {
		return nil, fmt.Errorf("create localdb directory: %w", err)
	}

	db, err := bbolt.Open(cleanPath, 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open localdb: %w", err)
	}

	wrapped := &DB{DB: db, path: cleanPath}
	if err := wrapped.init(); err != nil {
		_ = db.Close()
		return nil, err
	}

	openMu.Lock()
	openDBs[cleanPath] = &sharedDB{db: db, refs: 1}
	openMu.Unlock()

	return wrapped, nil
}

func (db *DB) Close() error {
	if db == nil || db.DB == nil {
		return nil
	}
	openMu.Lock()
	shared := openDBs[db.path]
	if shared == nil {
		openMu.Unlock()
		return nil
	}
	shared.refs--
	if shared.refs > 0 {
		openMu.Unlock()
		return nil
	}
	delete(openDBs, db.path)
	raw := shared.db
	openMu.Unlock()
	return raw.Close()
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
	prefix := []byte(fmt.Sprintf("%s|%s|", owner, blobID))
	err := db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket([]byte(BucketReceipts)).Cursor()
		for k, v := c.Seek(prefix); k != nil && bytesHasPrefix(k, prefix); k, v = c.Next() {
			var r receipts.Receipt
			if err := unmarshalReceipt(v, &r); err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	})
	return out, err
}

// ChallengeResult records the result of a challenge response.
func (db *DB) RecordChallengeResult(ctx context.Context, peer string, ref blobserver.ShardRef, success bool, proof string, timestamp time.Time) error {

	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketChallengeHistory))
		if b == nil {
			return errors.New("challenge_history bucket not found")
		}

		chr, err := newChallengeHistoryRecord(peer, ref.BlobID, success, proof)
		if err != nil {
			return err
		}
		chr.Timestamp = timestamp.UTC()

		data, err := marshalChallengeHistoryRecord(chr)
		if err != nil {
			return err
		}

		key := fmt.Sprintf("%s|%s|%d|%d", ownerOrDefault(ref.Owner), ref.BlobID, ref.Index, timestamp.UnixNano())
		return b.Put([]byte(key), data)
	})
}

// GetChallengeHistory returns challenge history for a shard.
func (db *DB) GetChallengeHistory(ctx context.Context, owner, blobID string) ([]ChallengeHistoryRecord, error) {
	var out []ChallengeHistoryRecord
	prefix := []byte(fmt.Sprintf("%s|%s|", ownerOrDefault(owner), blobID))
	err := db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket([]byte(BucketChallengeHistory)).Cursor()
		for k, v := c.Seek(prefix); k != nil && bytesHasPrefix(k, prefix); k, v = c.Next() {
			var r ChallengeHistoryRecord
			if err := unmarshalChallengeHistoryRecord(v, &r); err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	})
	return out, err
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
	key := fmt.Sprintf("%s|%s|%d", owner, blobID, 0)
	h.records[key] = append(h.records[key], ChallengeHistoryRecord{
		Peer:      peer,
		Success:   success,
		Proof:     proof,
		Nonce:     nonce,
		Timestamp: timestamp,
	})
}

// Get returns challenge history for a shard.
func (h *ChallengeHistory) Get(owner, blobID string) []ChallengeHistoryRecord {
	key := fmt.Sprintf("%s|%s|%d", owner, blobID, 0)
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
	return json.Marshal(r)
}

// unmarshalReceipt decodes bytes to a receipt.
func unmarshalReceipt(data []byte, r *receipts.Receipt) error {
	return json.Unmarshal(data, r)
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
		Peer:      peer,
		Success:   success,
		Proof:     proof,
		Timestamp: time.Now().UTC(),
	}, nil
}

// marshalChallengeHistoryRecord encodes a challenge history record.
func marshalChallengeHistoryRecord(r *ChallengeHistoryRecord) ([]byte, error) {
	return json.Marshal(r)
}

// unmarshalChallengeHistoryRecord decodes bytes to a challenge history record.
func unmarshalChallengeHistoryRecord(data []byte, r *ChallengeHistoryRecord) error {
	return json.Unmarshal(data, r)
}

func bytesHasPrefix(value, prefix []byte) bool {
	if len(value) < len(prefix) {
		return false
	}
	for i := range prefix {
		if value[i] != prefix[i] {
			return false
		}
	}
	return true
}

func ownerOrDefault(owner string) string {
	if owner == "" {
		return "_"
	}
	return owner
}
