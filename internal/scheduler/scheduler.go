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
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/secrets"
)

const CheckInterval = time.Minute

type Options struct {
	Config     config.Config
	ConfigPath string
	DB         *localdb.DB
	Output     io.Writer
}

func Start(ctx context.Context, opts Options) {
	out := opts.Output
	if out == nil {
		out = io.Discard
	}
	if opts.DB == nil {
		fmt.Fprintln(out, "backup scheduler unavailable: localdb is required")
		return
	}
	if strings.EqualFold(opts.Config.Backup.Schedule, "off") || strings.EqualFold(opts.Config.Backup.Schedule, "manual") {
		fmt.Fprintf(out, "backup scheduler: disabled (%s)\n", opts.Config.Backup.Schedule)
		return
	}
	if len(opts.Config.Backup.Roots) == 0 {
		fmt.Fprintln(out, "backup scheduler: no protected roots configured")
		return
	}
	interval, err := Interval(opts.Config.Backup.Schedule)
	if err != nil {
		fmt.Fprintf(out, "backup scheduler unavailable: %v\n", err)
		return
	}
	if opts.ConfigPath == "" {
		fmt.Fprintln(out, "backup scheduler unavailable: config path is required")
		return
	}
	fmt.Fprintf(out, "backup scheduler: %s (%s)\n", opts.Config.Backup.Schedule, interval)

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
		fmt.Fprintln(out, "backup scheduler: P2PBK_REPOSITORY_PASSWORD is not set; scheduled backups paused")
		return
	}
	engine := kopiaengine.New(opts.ConfigPath, opts.Config, password)
	for _, root := range opts.Config.Backup.Roots {
		if root == "" {
			continue
		}
		due, last, err := Due(opts.DB, root, interval, now)
		if err != nil {
			fmt.Fprintf(out, "backup scheduler: cannot read last backup for %s: %v\n", root, err)
			continue
		}
		if !due {
			continue
		}
		if last.IsZero() && !opts.Config.Backup.InitialFull {
			continue
		}
		fmt.Fprintf(out, "backup scheduler: starting backup for %s\n", root)
		result, err := engine.Backup(ctx, root, "scheduled")
		if err != nil {
			_ = opts.DB.PutSetting(LastFailureKey(root), []byte(now.Format(time.RFC3339Nano)+" "+err.Error()))
			fmt.Fprintf(out, "backup scheduler: backup failed for %s: %v\n", root, err)
			continue
		}
		_ = opts.DB.PutSetting(LastSuccessKey(root), []byte(now.Format(time.RFC3339Nano)))
		fmt.Fprintf(out, "backup scheduler: snapshot %s saved for %s\n", result.ManifestID, result.Source)
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
