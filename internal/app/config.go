package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"p2pbk/internal/backend/p2p/erasure"
	"p2pbk/internal/config"
	"p2pbk/internal/peer/blobserver"
)

type ConfigSummary struct {
	Path                     string
	DataDir                  string
	StoreDir                 string
	CacheDir                 string
	OfferBytes               int64
	ProtocolID               string
	MinPeers                 int
	Discovery                string
	LocalDiscoveryService    string
	RemoteDiscoveryNamespace string
	RemoteDiscoveryInterval  string
	PublicDiscoveryPeers     []string
	ProtectedRoots           []string
	MaxProtectedBytes        int64
	Schedule                 string
	Retention                string
	DataShards               int
	ParityShards             int
	ChallengeInterval        string
	ReputationWindow         string
	DisplayName              string
	BootstrapPeers           []string
}

func SummarizeConfig(cfg config.Config, path string) (ConfigSummary, error) {
	paths, err := cfg.Paths()
	if err != nil {
		return ConfigSummary{}, err
	}
	return ConfigSummary{
		Path:                     path,
		DataDir:                  paths.DataDir,
		StoreDir:                 paths.StoreDir,
		CacheDir:                 paths.CacheDir,
		OfferBytes:               cfg.Storage.OfferBytes,
		ProtocolID:               cfg.Network.ProtocolID,
		MinPeers:                 cfg.Network.MinPeers,
		Discovery:                cfg.Network.Discovery,
		LocalDiscoveryService:    cfg.Network.LocalDiscoveryService,
		RemoteDiscoveryNamespace: cfg.Network.RemoteDiscoveryNamespace,
		RemoteDiscoveryInterval:  cfg.Network.RemoteDiscoveryInterval,
		PublicDiscoveryPeers:     append([]string(nil), cfg.Network.PublicDiscoveryPeers...),
		ProtectedRoots:           NormalizeRootList(cfg.Backup.Roots),
		MaxProtectedBytes:        cfg.Backup.MaxBytes,
		Schedule:                 cfg.Backup.Schedule,
		Retention:                cfg.Backup.Retention,
		DataShards:               cfg.Backup.DataShards,
		ParityShards:             cfg.Backup.ParityShards,
		ChallengeInterval:        cfg.Trust.ChallengeInterval,
		ReputationWindow:         cfg.Trust.ReputationWindow,
		DisplayName:              cfg.Node.DisplayName,
		BootstrapPeers:           append([]string(nil), cfg.Network.BootstrapPeers...),
	}, nil
}

func AddAsset(cfg config.Config, configPath, path string) (config.Config, error) {
	pathsToAdd := SplitPathList(path)
	if len(pathsToAdd) == 0 {
		return cfg, errors.New("path is required")
	}
	paths, err := cfg.Paths()
	if err != nil {
		return cfg, err
	}
	if _, err := blobserver.New(paths.StoreDir); err != nil {
		return cfg, fmt.Errorf("initialize blobserver: %w", err)
	}
	if _, err := erasure.New(cfg.Backup.DataShards, cfg.Backup.ParityShards); err != nil {
		return cfg, fmt.Errorf("create codec: %w", err)
	}

	cfg.Backup.Roots = NormalizeRootList(cfg.Backup.Roots)
	for _, path := range pathsToAdd {
		if _, err := os.Stat(path); err != nil {
			return cfg, fmt.Errorf("path %q: %w", path, err)
		}
		clean := filepath.Clean(path)
		exists := false
		for _, root := range cfg.Backup.Roots {
			if samePath(root, clean) {
				exists = true
				break
			}
		}
		if !exists {
			cfg.Backup.Roots = append(cfg.Backup.Roots, clean)
		}
	}
	if configPath != "" {
		if err := SaveConfig(cfg, configPath); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func RemoveAsset(cfg config.Config, configPath, path string) (config.Config, error) {
	pathsToRemove := SplitPathList(path)
	if len(pathsToRemove) == 0 {
		return cfg, errors.New("path is required")
	}
	cfg.Backup.Roots = NormalizeRootList(cfg.Backup.Roots)
	removed := 0
	for _, removePath := range pathsToRemove {
		clean := filepath.Clean(removePath)
		next := cfg.Backup.Roots[:0]
		for _, root := range cfg.Backup.Roots {
			if samePath(root, clean) {
				removed++
				continue
			}
			next = append(next, root)
		}
		cfg.Backup.Roots = next
	}
	if removed == 0 {
		return cfg, fmt.Errorf("protected folder not found: %s", strings.Join(pathsToRemove, ";"))
	}
	if configPath != "" {
		if err := SaveConfig(cfg, configPath); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func NormalizeRootList(roots []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, root := range roots {
		for _, path := range SplitPathList(root) {
			clean := filepath.Clean(path)
			key := strings.ToLower(clean)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, clean)
		}
	}
	return out
}

func SplitPathList(value string) []string {
	parts := strings.Split(value, ";")
	out := []string{}
	for _, part := range parts {
		part = cleanPathInput(part)
		if part == "" {
			continue
		}
		out = append(out, filepath.Clean(part))
	}
	return out
}

func cleanPathInput(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	value = strings.TrimSpace(value)
	return value
}

func ApplyConfigSet(cfg *config.Config, key, value string) error {
	switch key {
	case "node.data_dir":
		cfg.Node.DataDir = value
	case "node.display_name":
		cfg.Node.DisplayName = value
	case "storage.store_dir":
		cfg.Storage.StoreDir = value
	case "storage.cache_dir":
		cfg.Storage.CacheDir = value
	case "storage.offer_bytes":
		n, err := ParseByteSize(value)
		if err != nil {
			return err
		}
		cfg.Storage.OfferBytes = n
	case "network.protocol_id":
		cfg.Network.ProtocolID = value
	case "network.discovery":
		cfg.Network.Discovery = value
	case "network.local_discovery_service":
		cfg.Network.LocalDiscoveryService = value
	case "network.remote_discovery_namespace":
		cfg.Network.RemoteDiscoveryNamespace = value
	case "network.remote_discovery_interval":
		cfg.Network.RemoteDiscoveryInterval = value
	case "network.public_discovery_peers":
		cfg.Network.PublicDiscoveryPeers = splitCommaList(value)
	case "backup.max_bytes":
		n, err := ParseByteSize(value)
		if err != nil {
			return err
		}
		cfg.Backup.MaxBytes = n
	case "backup.schedule":
		cfg.Backup.Schedule = value
	case "backup.retention":
		cfg.Backup.Retention = value
	case "trust.challenge_interval":
		cfg.Trust.ChallengeInterval = value
	case "trust.reputation_window":
		cfg.Trust.ReputationWindow = value
	default:
		return fmt.Errorf("unsupported config key %q", key)
	}
	return cfg.Validate()
}

func splitCommaList(value string) []string {
	parts := strings.Split(value, ",")
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func SaveConfig(cfg config.Config, path string) error {
	if path == "" {
		return errors.New("config path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func ParseByteSize(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, errors.New("size is required")
	}
	upper := strings.ToUpper(trimmed)
	multipliers := []struct {
		suffix string
		value  int64
	}{
		{"TIB", 1 << 40},
		{"TB", 1_000_000_000_000},
		{"GIB", 1 << 30},
		{"GB", 1_000_000_000},
		{"MIB", 1 << 20},
		{"MB", 1_000_000},
		{"KIB", 1 << 10},
		{"KB", 1_000},
		{"B", 1},
	}
	for _, multiplier := range multipliers {
		if strings.HasSuffix(upper, multiplier.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSpace(trimmed[:len(trimmed)-len(multiplier.suffix)]), 64)
			if err != nil {
				return 0, err
			}
			if n < 0 {
				return 0, errors.New("size must be non-negative")
			}
			return int64(n * float64(multiplier.value)), nil
		}
	}
	n, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, errors.New("size must be non-negative")
	}
	return n, nil
}

func FormatBytes(value int64) string {
	const gb = int64(1_000_000_000)
	if value >= gb && value%gb == 0 {
		return fmt.Sprintf("%dGB", value/gb)
	}
	return fmt.Sprintf("%d bytes", value)
}
