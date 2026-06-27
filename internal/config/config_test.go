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
	if cfg.Network.Discovery != "remote" {
		t.Fatalf("discovery = %q, want remote", cfg.Network.Discovery)
	}
	if cfg.Network.RemoteDiscoveryNamespace != defaultRemoteDiscoveryNamespace {
		t.Fatalf("remote namespace = %q, want %q", cfg.Network.RemoteDiscoveryNamespace, defaultRemoteDiscoveryNamespace)
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

func TestValidateRejectsInvalidRemoteDiscoveryInterval(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	cfg.Network.RemoteDiscoveryInterval = "not-a-duration"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate returned nil error for invalid remote discovery interval")
	}
}

func TestValidateRejectsInvalidDiscoveryMode(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	cfg.Network.Discovery = "both"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate returned nil error for invalid discovery mode")
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
	parsedInterval, err := time.ParseDuration(cfg.Trust.ChallengeInterval)
	if err != nil {
		t.Fatalf("failed to parse challenge interval: %v", err)
	}
	if parsedInterval != 2*time.Hour {
		t.Fatalf("challenge interval = %s, want %v", parsedInterval, 2*time.Hour)
	}
	parsedReputationWindow, err := time.ParseDuration(cfg.Trust.ReputationWindow)
	if err != nil {
		t.Fatalf("failed to parse reputation window: %v", err)
	}
	if parsedReputationWindow != 48*time.Hour {
		t.Fatalf("reputation window = %s, want %v", parsedReputationWindow, 48*time.Hour)
	}
}

func TestLoadParsesJSONConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := []byte(`{
  "node": {
    "data_dir": "/tmp/p2pbk-json",
    "identity": "identity.age",
    "display_name": "json-node"
  },
  "backup": {
    "roots": ["/tmp/source"],
    "max_bytes": 10000000000,
    "schedule": "daily",
    "retention": "30d"
  }
}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Node.DisplayName != "json-node" {
		t.Fatalf("display name = %q, want json-node", cfg.Node.DisplayName)
	}
	if cfg.Backup.MaxBytes != 10000000000 {
		t.Fatalf("max bytes = %d, want 10000000000", cfg.Backup.MaxBytes)
	}
	if got := cfg.Backup.Roots; len(got) != 1 || got[0] != "/tmp/source" {
		t.Fatalf("roots = %v, want [/tmp/source]", got)
	}
}
