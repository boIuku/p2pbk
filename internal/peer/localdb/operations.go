package localdb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

type OperationRecord struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	ConfigPath  string    `json:"config_path,omitempty"`
	Root        string    `json:"root,omitempty"`
	Snapshot    string    `json:"snapshot,omitempty"`
	State       string    `json:"state"`
	Attempt     int       `json:"attempt"`
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Error       string    `json:"error,omitempty"`
}

func (db *DB) PutOperation(record OperationRecord) error {
	if record.ID == "" {
		return errors.New("operation id is required")
	}
	if record.Kind == "" {
		return errors.New("operation kind is required")
	}
	if record.State == "" {
		return errors.New("operation state is required")
	}
	now := time.Now().UTC()
	if record.StartedAt.IsZero() {
		record.StartedAt = now
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal operation: %w", err)
	}
	_, err = db.conn.Exec(`
		INSERT INTO operations(id, kind, state, updated_at, data) VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET kind = excluded.kind, state = excluded.state, updated_at = excluded.updated_at, data = excluded.data`,
		record.ID, record.Kind, record.State, timeKey(record.UpdatedAt), data)
	return err
}

func (db *DB) GetOperation(id string) (*OperationRecord, error) {
	if id == "" {
		return nil, errors.New("operation id is required")
	}
	var data []byte
	err := db.conn.QueryRow(`SELECT data FROM operations WHERE id = ?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record OperationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("unmarshal operation: %w", err)
	}
	return &record, nil
}

func (db *DB) ListOperations(limit int) ([]OperationRecord, error) {
	query := `SELECT data FROM operations ORDER BY updated_at DESC`
	args := []any(nil)
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OperationRecord
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record OperationRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, fmt.Errorf("unmarshal operation: %w", err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}
