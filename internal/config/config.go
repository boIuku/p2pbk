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
	Node    NodeConfig    `mapstructure:"node"`
	Storage StorageConfig `mapstructure:"storage"`
	Network NetworkConfig `mapstructure:"network"`
	Backup  BackupConfig  `mapstructure:"backup"`
	Trust   TrustConfig   `mapstructure:"trust"`
}

type NodeConfig struct {
	DataDir     string `mapstructure:"data_dir"`
	Identity    string `mapstructure:"identity"`
	DisplayName string `mapstructure:"display_name"`
}

type StorageConfig struct {
	StoreDir       string `mapstructure:"store_dir"`
	CacheDir       string `mapstructure:"cache_dir"`
	CacheSizeBytes int64  `mapstructure:"cache_size_bytes"`
	OfferBytes     int64  `mapstructure:"offer_bytes"`
}

type NetworkConfig struct {
	ListenAddrs    []string `mapstructure:"listen_addrs"`
	BootstrapPeers []string `mapstructure:"bootstrap_peers"`
	ProtocolID     string   `mapstructure:"protocol_id"`
	MinPeers       int      `mapstructure:"min_peers"`
}

type BackupConfig struct {
	DataShards   int      `mapstructure:"data_shards"`
	ParityShards int      `mapstructure:"parity_shards"`
	Roots        []string `mapstructure:"roots"`
}

type TrustConfig struct {
	ChallengeInterval time.Duration `mapstructure:"challenge_interval"`
	ReputationWindow  time.Duration `mapstructure:"reputation_window"`
}

// Load reads config from path, applies defaults, overlays P2PBK_* environment
// variables, and validates the result.
func Load(path string) (Config, error) {
	v := viper.New()
	applyDefaults(v)

	v.SetConfigType("yaml")
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
	if c.Backup.DataShards <= 0 {
		errs = append(errs, errors.New("backup.data_shards must be positive"))
	}
	if c.Backup.ParityShards <= 0 {
		errs = append(errs, errors.New("backup.parity_shards must be positive"))
	}
	if c.Trust.ChallengeInterval <= 0 {
		errs = append(errs, errors.New("trust.challenge_interval must be positive"))
	}
	if c.Trust.ReputationWindow <= 0 {
		errs = append(errs, errors.New("trust.reputation_window must be positive"))
	}

	return errors.Join(errs...)
}
