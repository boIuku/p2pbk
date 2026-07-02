package localdb

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
	path string
}

func Open(path string) (*DB, error) {
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve localdb path: %w", err)
	}
	cleanPath = filepath.Clean(cleanPath)

	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o700); err != nil {
		return nil, fmt.Errorf("create localdb directory: %w", err)
	}

	db, err := openSQLite(cleanPath)
	if err != nil {
		return nil, err
	}
	if err := db.init(); err != nil {
		_ = db.Close()
		if !isSQLiteFormatError(err) {
			return nil, err
		}
		backup, renameErr := moveAsideLegacyDB(cleanPath)
		if renameErr != nil {
			return nil, fmt.Errorf("%w; additionally failed to move legacy localdb aside: %v", err, renameErr)
		}
		db, err = openSQLite(cleanPath)
		if err != nil {
			return nil, err
		}
		if err := db.init(); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize fresh sqlite localdb after moving legacy db to %s: %w", backup, err)
		}
	}
	return db, nil
}

func openSQLite(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open localdb: %w", err)
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(0)
	return &DB{conn: conn, path: path}, nil
}

func (db *DB) Close() error {
	if db == nil || db.conn == nil {
		return nil
	}
	return db.conn.Close()
}

func (db *DB) init() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, stmt := range pragmas {
		if _, err := db.conn.Exec(stmt); err != nil {
			return fmt.Errorf("apply sqlite pragma %q: %w", stmt, err)
		}
	}
	schema := []string{
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value BLOB NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS peers (
			id TEXT PRIMARY KEY,
			last_seen TEXT NOT NULL,
			data BLOB NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS peers_last_seen_idx ON peers(last_seen)`,
		`CREATE TABLE IF NOT EXISTS owned_blobs (
			key TEXT PRIMARY KEY,
			data BLOB NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS shard_receipts (
			key TEXT PRIMARY KEY,
			peer_id TEXT NOT NULL,
			stored_at TEXT NOT NULL,
			data BLOB NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS shard_receipts_stored_at_idx ON shard_receipts(stored_at)`,
		`CREATE TABLE IF NOT EXISTS audit_history (
			key TEXT PRIMARY KEY,
			peer_id TEXT NOT NULL,
			checked_at TEXT NOT NULL,
			data BLOB NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS audit_history_peer_checked_idx ON audit_history(peer_id, checked_at)`,
	}
	for _, stmt := range schema {
		if _, err := db.conn.Exec(stmt); err != nil {
			return fmt.Errorf("initialize localdb schema: %w", err)
		}
	}
	return nil
}

func isSQLiteFormatError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "not a database")
}

func moveAsideLegacyDB(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	backup := path + ".legacy.bak"
	for i := 1; ; i++ {
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			return backup, os.Rename(path, backup)
		}
		backup = fmt.Sprintf("%s.legacy.%d.bak", path, i)
	}
}

func (db *DB) PutSetting(key string, value []byte) error {
	if key == "" {
		return errors.New("setting key is required")
	}
	_, err := db.conn.Exec(
		`INSERT INTO settings(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key,
		append([]byte(nil), value...),
	)
	return err
}

func (db *DB) GetSetting(key string) ([]byte, error) {
	if key == "" {
		return nil, errors.New("setting key is required")
	}

	var value []byte
	err := db.conn.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), value...), nil
}

func timeKey(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
