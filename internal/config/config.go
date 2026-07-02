package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"
)

// Config contains all user-tunable settings for a p2pbk node.
type Config struct {
	Node    NodeConfig    `mapstructure:"node" json:"node"`
	Storage StorageConfig `mapstructure:"storage" json:"storage"`
	Network NetworkConfig `mapstructure:"network" json:"network"`
	Backup  BackupConfig  `mapstructure:"backup" json:"backup"`
	Trust   TrustConfig   `mapstructure:"trust" json:"trust"`
}

type NodeConfig struct {
	DataDir     string `mapstructure:"data_dir" json:"data_dir"`
	Identity    string `mapstructure:"identity" json:"identity"`
	DisplayName string `mapstructure:"display_name" json:"display_name"`
}

type StorageConfig struct {
	StoreDir       string `mapstructure:"store_dir" json:"store_dir"`
	CacheDir       string `mapstructure:"cache_dir" json:"cache_dir"`
	CacheSizeBytes int64  `mapstructure:"cache_size_bytes" json:"cache_size_bytes"`
	OfferBytes     int64  `mapstructure:"offer_bytes" json:"offer_bytes"`
}

type NetworkConfig struct {
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

// Load reads config from path, applies defaults, and validates.
func Load(path string) (Config, error) {
	v := viper.New()
	applyDefaults(v)

	v.SetEnvPrefix("P2PBK")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if path != "" {
		v.SetConfigFile(path)
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

	return errors.Join(errs...)
}
