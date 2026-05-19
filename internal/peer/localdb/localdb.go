package localdb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

type DB struct {
	*bolt.DB
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create localdb directory: %w", err)
	}

	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
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
	return db.Update(func(tx *bolt.Tx) error {
		for _, name := range RequiredBuckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("create bucket %s: %w", name, err)
			}
		}
		return nil
	})
}

func (db *DB) PutSetting(key string, value []byte) error {
	if key == "" {
		return errors.New("setting key is required")
	}
	return db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(BucketSettings)).Put([]byte(key), value)
	})
}

func (db *DB) GetSetting(key string) ([]byte, error) {
	if key == "" {
		return nil, errors.New("setting key is required")
	}

	var out []byte
	err := db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket([]byte(BucketSettings)).Get([]byte(key))
		if value == nil {
			return nil
		}
		out = append([]byte(nil), value...)
		return nil
	})
	return out, err
}
