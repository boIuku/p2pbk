package config

import (
	"time"

	"github.com/spf13/viper"
)

const (
	defaultDataDir    = "~/.p2pbk"
	defaultProtocolID = "/p2pbk/1.0.0"
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
	v.SetDefault("network.protocol_id", defaultProtocolID)
	v.SetDefault("network.min_peers", 3)

	v.SetDefault("backup.data_shards", 6)
	v.SetDefault("backup.parity_shards", 3)
	v.SetDefault("backup.roots", []string{})

	v.SetDefault("trust.challenge_interval", time.Hour)
	v.SetDefault("trust.reputation_window", 30*24*time.Hour)
}
