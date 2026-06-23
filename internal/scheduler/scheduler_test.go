package scheduler

import (
	"path/filepath"
	"testing"
	"time"

	"p2pbk/internal/peer/localdb"
)

func TestInterval(t *testing.T) {
	tests := []struct {
		schedule string
		want     time.Duration
	}{
		{schedule: "hourly", want: time.Hour},
		{schedule: "daily", want: 24 * time.Hour},
		{schedule: "weekly", want: 7 * 24 * time.Hour},
		{schedule: "15m", want: 15 * time.Minute},
	}
	for _, tt := range tests {
		got, err := Interval(tt.schedule)
		if err != nil {
			t.Fatalf("Interval(%q) returned error: %v", tt.schedule, err)
		}
		if got != tt.want {
			t.Fatalf("Interval(%q) = %s, want %s", tt.schedule, got, tt.want)
		}
	}
}

func TestDueUsesLastSuccess(t *testing.T) {
	db, err := localdb.Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	root := "/tmp/source"
	now := time.Unix(3600, 0).UTC()
	due, _, err := Due(db, root, time.Hour, now)
	if err != nil {
		t.Fatalf("Due returned error: %v", err)
	}
	if !due {
		t.Fatal("Due = false before first backup")
	}

	if err := db.PutSetting(LastSuccessKey(root), []byte(now.Format(time.RFC3339Nano))); err != nil {
		t.Fatalf("PutSetting returned error: %v", err)
	}
	due, _, err = Due(db, root, time.Hour, now.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("Due returned error after last success: %v", err)
	}
	if due {
		t.Fatal("Due = true before interval elapsed")
	}
	due, _, err = Due(db, root, time.Hour, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Due returned error after interval: %v", err)
	}
	if !due {
		t.Fatal("Due = false after interval elapsed")
	}
}
