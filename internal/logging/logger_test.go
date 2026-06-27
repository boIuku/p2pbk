package logging

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestLoggerWritesStructuredEvent(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "daemon")
	logger.now = func() time.Time { return time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC) }

	logger.Write(Event{
		Event:       "shard_checked",
		PeerID:      "peer-1",
		ShardRef:    &ShardRef{Owner: "owner", BlobID: "blob", Index: 0},
		OperationID: "op-1",
		DurationMS:  12,
		Fields:      Field{"ok": true},
	})

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("log line is not JSON: %v\n%s", err, out.String())
	}
	if got["component"] != "daemon" {
		t.Fatalf("component = %v, want daemon", got["component"])
	}
	if got["event"] != "shard_checked" {
		t.Fatalf("event = %v, want shard_checked", got["event"])
	}
	ref, ok := got["shard_ref"].(map[string]any)
	if !ok {
		t.Fatalf("shard_ref missing or invalid: %#v", got["shard_ref"])
	}
	if ref["index"] != float64(0) {
		t.Fatalf("shard_ref.index = %v, want 0", ref["index"])
	}
}

func TestLoggerWithComponentSharesWriter(t *testing.T) {
	var out bytes.Buffer
	root := New(&out, "daemon")
	root.WithComponent("scheduler").Info("started", "scheduler started", nil)

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	if got["component"] != "scheduler" {
		t.Fatalf("component = %v, want scheduler", got["component"])
	}
}
