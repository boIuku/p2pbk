package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"p2pbk/internal/config"
	"p2pbk/internal/kopiaengine"
	"p2pbk/internal/logging"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/secrets"
)

const CheckInterval = time.Minute

type Options struct {
	Config     config.Config
	ConfigPath string
	DB         *localdb.DB
	Output     io.Writer
	Logger     *logging.Logger
}

func Start(ctx context.Context, opts Options) {
	out := opts.Output
	if out == nil {
		out = io.Discard
	}
	if opts.DB == nil {
		logSchedulerError(opts, out, "scheduler_unavailable", "backup scheduler unavailable: localdb is required", "localdb_required", nil, nil)
		return
	}
	if strings.EqualFold(opts.Config.Backup.Schedule, "off") || strings.EqualFold(opts.Config.Backup.Schedule, "manual") {
		logSchedulerInfo(opts, out, "scheduler_disabled", fmt.Sprintf("backup scheduler: disabled (%s)", opts.Config.Backup.Schedule), logging.Field{"schedule": opts.Config.Backup.Schedule})
		return
	}
	if len(opts.Config.Backup.Roots) == 0 {
		logSchedulerInfo(opts, out, "scheduler_no_roots", "backup scheduler: no protected roots configured", nil)
		return
	}
	interval, err := Interval(opts.Config.Backup.Schedule)
	if err != nil {
		logSchedulerError(opts, out, "scheduler_unavailable", "backup scheduler unavailable", "invalid_schedule", err, logging.Field{"schedule": opts.Config.Backup.Schedule})
		return
	}
	if opts.ConfigPath == "" {
		logSchedulerError(opts, out, "scheduler_unavailable", "backup scheduler unavailable: config path is required", "config_path_required", nil, nil)
		return
	}
	logSchedulerInfo(opts, out, "scheduler_started", fmt.Sprintf("backup scheduler: %s (%s)", opts.Config.Backup.Schedule, interval), logging.Field{
		"schedule":    opts.Config.Backup.Schedule,
		"interval_ms": interval.Milliseconds(),
	})

	go func() {
		RunDue(ctx, opts, interval, time.Now().UTC())
		ticker := time.NewTicker(CheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				RunDue(ctx, opts, interval, now.UTC())
			}
		}
	}()
}

func RunDue(ctx context.Context, opts Options, interval time.Duration, now time.Time) {
	out := opts.Output
	if out == nil {
		out = io.Discard
	}
	password := secrets.FromEnv()
	if password == "" {
		logSchedulerError(opts, out, "scheduler_paused", "backup scheduler: P2PBK_REPOSITORY_PASSWORD is not set; scheduled backups paused", "password_required", nil, nil)
		return
	}
	engine := kopiaengine.New(opts.ConfigPath, opts.Config, password)
	for _, root := range opts.Config.Backup.Roots {
		if root == "" {
			continue
		}
		due, last, err := Due(opts.DB, root, interval, now)
		if err != nil {
			logSchedulerError(opts, out, "scheduled_backup_skipped", "backup scheduler: cannot read last backup", "history_read_failed", err, logging.Field{"root": root})
			continue
		}
		if !due {
			continue
		}
		if last.IsZero() && !opts.Config.Backup.InitialFull {
			continue
		}
		operationID := "scheduled-backup-" + rootKey(root) + "-" + now.Format("20060102T150405Z")
		started := time.Now()
		logSchedulerEvent(opts, out, logging.Event{
			Level:       "info",
			Event:       "scheduled_backup_started",
			Message:     "backup scheduler: starting backup",
			OperationID: operationID,
			Fields:      logging.Field{"root": root},
		}, fmt.Sprintf("backup scheduler: starting backup for %s", root))
		result, err := engine.Backup(ctx, root, "scheduled")
		duration := time.Since(started)
		if err != nil {
			_ = opts.DB.PutSetting(LastFailureKey(root), []byte(now.Format(time.RFC3339Nano)+" "+err.Error()))
			logSchedulerEvent(opts, out, logging.Event{
				Level:       "error",
				Event:       "scheduled_backup_failed",
				Message:     "backup scheduler: backup failed",
				OperationID: operationID,
				DurationMS:  duration.Milliseconds(),
				ErrorCode:   "backup_failed",
				Error:       err.Error(),
				Fields:      logging.Field{"root": root},
			}, fmt.Sprintf("backup scheduler: backup failed for %s: %v", root, err))
			continue
		}
		_ = opts.DB.PutSetting(LastSuccessKey(root), []byte(now.Format(time.RFC3339Nano)))
		logSchedulerEvent(opts, out, logging.Event{
			Level:       "info",
			Event:       "scheduled_backup_completed",
			Message:     "backup scheduler: snapshot saved",
			OperationID: operationID,
			DurationMS:  duration.Milliseconds(),
			Fields: logging.Field{
				"root":        root,
				"snapshot_id": string(result.ManifestID),
				"source":      fmt.Sprint(result.Source),
			},
		}, fmt.Sprintf("backup scheduler: snapshot %s saved for %s", result.ManifestID, result.Source))
	}
}

func logSchedulerInfo(opts Options, out io.Writer, event, message string, fields logging.Field) {
	logSchedulerEvent(opts, out, logging.Event{Level: "info", Event: event, Message: message, Fields: fields}, message)
}

func logSchedulerError(opts Options, out io.Writer, event, message, code string, err error, fields logging.Field) {
	logEvent := logging.Event{Level: "error", Event: event, Message: message, ErrorCode: code, Fields: fields}
	if err != nil {
		logEvent.Error = err.Error()
	}
	plain := message
	if err != nil {
		plain = fmt.Sprintf("%s: %v", message, err)
	}
	logSchedulerEvent(opts, out, logEvent, plain)
}

func logSchedulerEvent(opts Options, out io.Writer, event logging.Event, plain string) {
	if opts.Logger != nil {
		opts.Logger.Write(event)
		return
	}
	if out != nil {
		fmt.Fprintln(out, plain)
	}
}

func Due(db *localdb.DB, root string, interval time.Duration, now time.Time) (bool, time.Time, error) {
	value, err := db.GetSetting(LastSuccessKey(root))
	if err != nil {
		return false, time.Time{}, err
	}
	if len(value) == 0 {
		return true, time.Time{}, nil
	}
	last, err := time.Parse(time.RFC3339Nano, string(value))
	if err != nil {
		return true, time.Time{}, nil
	}
	return !now.Before(last.Add(interval)), last, nil
}

func Interval(schedule string) (time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(schedule)) {
	case "hourly":
		return time.Hour, nil
	case "daily":
		return 24 * time.Hour, nil
	case "weekly":
		return 7 * 24 * time.Hour, nil
	}
	interval, err := time.ParseDuration(schedule)
	if err != nil {
		return 0, fmt.Errorf("unsupported backup schedule %q", schedule)
	}
	if interval <= 0 {
		return 0, fmt.Errorf("backup schedule must be positive")
	}
	return interval, nil
}

func LastSuccessKey(root string) string {
	return "backup.last_success." + rootKey(root)
}

func LastFailureKey(root string) string {
	return "backup.last_failure." + rootKey(root)
}

func rootKey(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:])
}
