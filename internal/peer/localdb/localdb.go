package localdb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.etcd.io/bbolt"
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
