package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"p2pbk/internal/durable"
)

// Config contains all user-tunable settings for a p2pbk node.
type Config struct {
	Node     NodeConfig     `mapstructure:"node" json:"node"`
	Storage  StorageConfig  `mapstructure:"storage" json:"storage"`
	Network  NetworkConfig  `mapstructure:"network" json:"network"`
	Backup   BackupConfig   `mapstructure:"backup" json:"backup"`
	Trust    TrustConfig    `mapstructure:"trust" json:"trust"`
	Recovery RecoveryConfig `mapstructure:"recovery" json:"recovery"`
}

type NodeConfig struct {
	DataDir     string `mapstructure:"data_dir" json:"data_dir"`
	Identity    string `mapstructure:"identity" json:"identity"`
	DisplayName string `mapstructure:"display_name" json:"display_name"`
}

type StorageConfig struct {
	Mode               string `mapstructure:"mode" json:"mode"`
	StoreDir           string `mapstructure:"store_dir" json:"store_dir"`
	CacheDir           string `mapstructure:"cache_dir" json:"cache_dir"`
	CacheSizeBytes     int64  `mapstructure:"cache_size_bytes" json:"cache_size_bytes"`
	OfferBytes         int64  `mapstructure:"offer_bytes" json:"offer_bytes"`
	PerOwnerOfferBytes int64  `mapstructure:"per_owner_offer_bytes" json:"per_owner_offer_bytes"`
}

type NetworkConfig struct {
	AnnounceAddrs                    []string `mapstructure:"announce_addrs" json:"announce_addrs"`
	ListenAddrs                      []string `mapstructure:"listen_addrs" json:"listen_addrs"`
	BootstrapPeers                   []string `mapstructure:"bootstrap_peers" json:"bootstrap_peers"`
	PublicDiscoveryPeers             []string `mapstructure:"public_discovery_peers" json:"public_discovery_peers"`
	Discovery                        string   `mapstructure:"discovery" json:"discovery"`
	ProtocolID                       string   `mapstructure:"protocol_id" json:"protocol_id"`
	MinPeers                         int      `mapstructure:"min_peers" json:"min_peers"`
	LocalDiscoveryService            string   `mapstructure:"local_discovery_service" json:"local_discovery_service"`
	RemoteDiscoveryNamespace         string   `mapstructure:"remote_discovery_namespace" json:"remote_discovery_namespace"`
	RemoteDiscoveryInterval          string   `mapstructure:"remote_discovery_interval" json:"remote_discovery_interval"`
	ForcePrivateReachabilityForRelay bool     `mapstructure:"force_private_reachability_for_relay" json:"force_private_reachability_for_relay"`
}

type BackupConfig struct {
	DataShards   int      `mapstructure:"data_shards" json:"data_shards"`
	ParityShards int      `mapstructure:"parity_shards" json:"parity_shards"`
	Roots        []string `mapstructure:"roots" json:"roots"`
	MaxBytes     int64    `mapstructure:"max_bytes" json:"max_bytes"`
	Schedule     string   `mapstructure:"schedule" json:"schedule"`
	Retention    string   `mapstructure:"retention" json:"retention"`
	InitialFull  bool     `mapstructure:"initial_full" json:"initial_full"`
	Incremental  bool     `mapstructure:"incremental" json:"incremental"`
}

type TrustConfig struct {
	ChallengeInterval  string `mapstructure:"challenge_interval" json:"challenge_interval"`
	ReputationWindow   string `mapstructure:"reputation_window" json:"reputation_window"`
	ChallengeMaxLength int64  `mapstructure:"challenge_max_length" json:"challenge_max_length"`
	IdentityPassphrase string `mapstructure:"identity_passphrase" json:"identity_passphrase"`
}

type RecoveryConfig struct {
	ExternalPath      string `mapstructure:"external_path" json:"external_path"`
	RetainGenerations int    `mapstructure:"retain_generations" json:"retain_generations"`
	RetryInterval     string `mapstructure:"retry_interval" json:"retry_interval"`
}

// Load reads config from path, applies defaults, and validates.
func Load(path string) (Config, error) {
	v := viper.New()
	applyDefaults(v)

	v.SetEnvPrefix("P2PBK")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if path != "" {
		v.SetConfigFile(path)
		if err := validateConfigKeys(path); err != nil {
			return Config{}, err
		}
		if err := v.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg, viper.DecodeHook(mapstructure.StringToTimeDurationHookFunc())); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks cross-field and range constraints.
func (c Config) Validate() error {
	var errs []error
	if c.Storage.Mode != "" && c.Storage.Mode != "auto" && c.Storage.Mode != "local" && c.Storage.Mode != "remote" {
		errs = append(errs, errors.New("storage.mode must be auto, local, or remote"))
	}

	if c.Node.DataDir == "" {
		errs = append(errs, errors.New("node.data_dir is required"))
	}
	if c.Storage.StoreDir == "" {
		errs = append(errs, errors.New("storage.store_dir is required"))
	}
	if c.Storage.CacheSizeBytes < 0 {
		errs = append(errs, errors.New("storage.cache_size_bytes must be non-negative"))
	}
	if c.Storage.OfferBytes < 0 {
		errs = append(errs, errors.New("storage.offer_bytes must be non-negative"))
	}
	if c.Storage.PerOwnerOfferBytes < 0 {
		errs = append(errs, errors.New("storage.per_owner_offer_bytes must be non-negative"))
	}
	if len(c.Network.ListenAddrs) == 0 {
		errs = append(errs, errors.New("network.listen_addrs must contain at least one address"))
	}
	if c.Network.ProtocolID == "" {
		errs = append(errs, errors.New("network.protocol_id is required"))
	}
	if c.Network.MinPeers < 0 {
		errs = append(errs, errors.New("network.min_peers must be non-negative"))
	}
	switch c.Network.Discovery {
	case "remote":
		if c.Network.RemoteDiscoveryNamespace == "" {
			errs = append(errs, errors.New("network.remote_discovery_namespace is required when network.discovery is remote"))
		}
		if c.Network.RemoteDiscoveryInterval == "" {
			errs = append(errs, errors.New("network.remote_discovery_interval is required when network.discovery is remote"))
		}
		if interval, err := time.ParseDuration(c.Network.RemoteDiscoveryInterval); err != nil || interval <= 0 {
			errs = append(errs, errors.New("network.remote_discovery_interval must be a positive duration"))
		}
	case "local":
		if c.Network.LocalDiscoveryService == "" {
			errs = append(errs, errors.New("network.local_discovery_service is required when network.discovery is local"))
		}
	case "disabled":
	default:
		errs = append(errs, fmt.Errorf("network.discovery must be remote, local, or disabled"))
	}
	if c.Backup.DataShards <= 0 {
		errs = append(errs, errors.New("backup.data_shards must be positive"))
	}
	if c.Backup.ParityShards <= 0 {
		errs = append(errs, errors.New("backup.parity_shards must be positive"))
	}
	if c.Backup.MaxBytes < 0 {
		errs = append(errs, errors.New("backup.max_bytes must be non-negative"))
	}
	if c.Backup.Schedule == "" {
		errs = append(errs, errors.New("backup.schedule is required"))
	}
	if c.Trust.ChallengeMaxLength <= 0 {
		errs = append(errs, errors.New("trust.challenge_max_length must be positive"))
	}
	if c.Recovery.RetainGenerations <= 0 {
		errs = append(errs, errors.New("recovery.retain_generations must be positive"))
	}
	if c.Recovery.RetryInterval == "" {
		errs = append(errs, errors.New("recovery.retry_interval is required"))
	} else if interval, err := time.ParseDuration(c.Recovery.RetryInterval); err != nil || interval <= 0 {
		errs = append(errs, errors.New("recovery.retry_interval must be a positive duration"))
	}

	return errors.Join(errs...)
}

var configKeys = map[string]map[string]struct{}{
	"node": {
		"data_dir": {}, "identity": {}, "display_name": {},
	},
	"storage": {
		"mode": {}, "store_dir": {}, "cache_dir": {}, "cache_size_bytes": {},
		"offer_bytes": {}, "per_owner_offer_bytes": {},
	},
	"network": {
		"announce_addrs": {}, "listen_addrs": {}, "bootstrap_peers": {},
		"public_discovery_peers": {}, "discovery": {}, "protocol_id": {},
		"min_peers": {}, "local_discovery_service": {},
		"remote_discovery_namespace": {}, "remote_discovery_interval": {},
		"force_private_reachability_for_relay": {},
	},
	"backup": {
		"data_shards": {}, "parity_shards": {}, "roots": {}, "max_bytes": {},
		"schedule": {}, "retention": {}, "initial_full": {}, "incremental": {},
	},
	"trust": {
		"challenge_interval": {}, "reputation_window": {},
		"challenge_max_length": {}, "identity_passphrase": {},
	},
	"recovery": {
		"external_path": {}, "retain_generations": {}, "retry_interval": {},
	},
}

func validateConfigKeys(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config for validation: %w", err)
	}
	var raw map[string]any
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".json" {
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		if err := decoder.Decode(&raw); err != nil {
			return fmt.Errorf("decode config for validation: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("decode config for validation: %w", err)
		}
	}
	for section, value := range raw {
		allowed, ok := configKeys[section]
		if !ok {
			return fmt.Errorf("unsupported config key %q", section)
		}
		fields, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("config section %q must be an object", section)
		}
		for key := range fields {
			if _, ok := allowed[key]; !ok {
				return fmt.Errorf("unsupported config key %q.%s", section, key)
			}
		}
	}
	return nil
}

// Save writes a validated configuration as a durable replacement. The
// identity passphrase is accepted only at the process boundary for legacy
// callers and is never persisted in plaintext configuration.
func Save(cfg Config, path string) error {
	if path == "" {
		return errors.New("config path is required")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	toWrite := cfg
	toWrite.Trust.IdentityPassphrase = ""
	data, err := json.MarshalIndent(toWrite, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := durable.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config atomically: %w", err)
	}
	return nil
}
