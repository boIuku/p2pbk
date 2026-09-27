package localdb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"p2pbk/internal/peer/blobserver"
)

type RepairJob struct {
	ID            string              `json:"id"`
	PeerID        string              `json:"peer_id"`
	Ref           blobserver.ShardRef `json:"ref"`
	Reason        string              `json:"reason"`
	State         string              `json:"state"`
	Attempts      int                 `json:"attempts"`
	NextAttemptAt time.Time           `json:"next_attempt_at"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
	LastError     string              `json:"last_error,omitempty"`
}

func (db *DB) EnqueueRepairJob(job RepairJob) error {
	if job.PeerID == "" {
		return errors.New("repair peer id is required")
	}
	if err := job.Ref.Validate(); err != nil {
		return err
	}
	if job.ID == "" {
		job.ID = repairJobKey(job.PeerID, job.Ref)
	}
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	if job.NextAttemptAt.IsZero() {
		job.NextAttemptAt = now
	}
	if job.State == "" {
		job.State = "pending"
	}
	var previousData []byte
	_ = db.conn.QueryRow(`SELECT data FROM repair_jobs WHERE key = ?`, job.ID).Scan(&previousData)
	if len(previousData) > 0 {
		var previous RepairJob
		if err := json.Unmarshal(previousData, &previous); err == nil {
			job.Attempts = previous.Attempts
			job.CreatedAt = previous.CreatedAt
			if previous.State == "completed" {
				job.Attempts = 0
			}
		}
	}
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal repair job: %w", err)
	}
	_, err = db.conn.Exec(`
		INSERT INTO repair_jobs(key, state, next_attempt_at, updated_at, data) VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET state = excluded.state, next_attempt_at = excluded.next_attempt_at, updated_at = excluded.updated_at, data = excluded.data`,
		job.ID, job.State, timeKey(job.NextAttemptAt), timeKey(job.UpdatedAt), data)
	return err
}

func (db *DB) CompleteRepairJob(peerID string, ref blobserver.ShardRef, at time.Time) error {
	job, err := db.GetRepairJob(repairJobKey(peerID, ref))
	if err != nil || job == nil {
		return err
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	job.State = "completed"
	job.UpdatedAt = at.UTC()
	job.NextAttemptAt = at.UTC()
	job.LastError = ""
	return db.EnqueueRepairJob(*job)
}

func (db *DB) FailRepairJob(peerID string, ref blobserver.ShardRef, reason string, at time.Time) error {
	job, err := db.GetRepairJob(repairJobKey(peerID, ref))
	if err != nil {
		return err
	}
	if job == nil {
		job = &RepairJob{PeerID: peerID, Ref: ref}
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	job.State = "pending"
	job.Attempts++
	job.LastError = reason
	job.UpdatedAt = at.UTC()
	backoff := time.Second << minRepairShift(job.Attempts)
	if backoff > time.Hour {
		backoff = time.Hour
	}
	job.NextAttemptAt = at.UTC().Add(backoff)
	return db.EnqueueRepairJob(*job)
}

func (db *DB) GetRepairJob(id string) (*RepairJob, error) {
	if id == "" {
		return nil, errors.New("repair job id is required")
	}
	var data []byte
	err := db.conn.QueryRow(`SELECT data FROM repair_jobs WHERE key = ?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var job RepairJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("unmarshal repair job: %w", err)
	}
	return &job, nil
}

func (db *DB) ListRepairJobs(state string, limit int) ([]RepairJob, error) {
	query := `SELECT data FROM repair_jobs`
	args := []any{}
	if state != "" {
		query += ` WHERE state = ?`
		args = append(args, state)
	}
	query += ` ORDER BY next_attempt_at ASC, updated_at ASC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RepairJob
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var job RepairJob
		if err := json.Unmarshal(data, &job); err != nil {
			return nil, fmt.Errorf("unmarshal repair job: %w", err)
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].NextAttemptAt.Before(out[j].NextAttemptAt) })
	return out, nil
}

func repairJobKey(peerID string, ref blobserver.ShardRef) string {
	return fmt.Sprintf("%s|%s|%s|%d", peerID, ref.Owner, ref.BlobID, ref.Index)
}

func minRepairShift(attempts int) uint {
	if attempts < 0 {
		return 0
	}
	if attempts > 16 {
		return 16
	}
	return uint(attempts)
}
