package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Network.ProtocolID != defaultProtocolID {
		t.Fatalf("protocol id = %q, want %q", cfg.Network.ProtocolID, defaultProtocolID)
	}
	if cfg.Backup.DataShards != 6 {
		t.Fatalf("data shards = %d, want 6", cfg.Backup.DataShards)
	}
}

func TestValidateRejectsInvalidShardCounts(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	cfg.Backup.DataShards = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate returned nil error for invalid data shard count")
	}
}

func TestLoadParsesDurationStrings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`trust:
  challenge_interval: 2h
  reputation_window: 48h
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Trust.ChallengeInterval != 2*time.Hour {
		t.Fatalf("challenge interval = %s", cfg.Trust.ChallengeInterval)
	}
	if cfg.Trust.ReputationWindow != 48*time.Hour {
		t.Fatalf("reputation window = %s", cfg.Trust.ReputationWindow)
	}
}
