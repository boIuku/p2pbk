package config

import (
	"time"

	"github.com/spf13/viper"
)

const (
	defaultDataDir    = "~/.p2pbk"
	defaultProtocolID = "/p2pbk/1.0.0"
	defaultRemoteDiscoveryNamespace = "p2pbk/storage/v1"
)

func applyDefaults(v *viper.Viper) {
	v.SetDefault("node.data_dir", defaultDataDir)
	v.SetDefault("node.identity", "identity.age")
	v.SetDefault("node.display_name", "")

	v.SetDefault("storage.store_dir", defaultDataDir+"/store")
	v.SetDefault("storage.cache_dir", defaultDataDir+"/cache")
	v.SetDefault("storage.cache_size_bytes", int64(1<<30))
	v.SetDefault("storage.offer_bytes", int64(10<<30))

	v.SetDefault("network.listen_addrs", []string{"/ip4/0.0.0.0/tcp/0", "/ip4/0.0.0.0/udp/0/quic-v1"})
	v.SetDefault("network.bootstrap_peers", []string{})
	v.SetDefault("network.public_discovery_peers", []string{})
	v.SetDefault("network.protocol_id", defaultProtocolID)
	v.SetDefault("network.min_peers", 3)
	v.SetDefault("network.enable_local_discovery", true)
	v.SetDefault("network.local_discovery_service", "_p2pbk._udp")
	v.SetDefault("network.enable_remote_discovery", true)
	v.SetDefault("network.remote_discovery_namespace", defaultRemoteDiscoveryNamespace)
	v.SetDefault("network.remote_discovery_interval", "1m")
	v.SetDefault("network.force_private_reachability_for_relay", false)

	v.SetDefault("backup.data_shards", 6)
	v.SetDefault("backup.parity_shards", 3)
	v.SetDefault("backup.roots", []string{})
	v.SetDefault("backup.max_bytes", int64(10<<30))
	v.SetDefault("backup.schedule", "daily")
	v.SetDefault("backup.retention", "30d")
	v.SetDefault("backup.initial_full", true)
	v.SetDefault("backup.incremental", true)

	v.SetDefault("trust.challenge_interval", time.Hour)
	v.SetDefault("trust.reputation_window", 30*24*time.Hour)
	v.SetDefault("trust.challenge_max_length", int64(64*1024))
	v.SetDefault("trust.identity_passphrase", "")
}
