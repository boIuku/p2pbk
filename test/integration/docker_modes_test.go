//go:build docker

package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDockerDiscoveryDisabledUsesExplicitPeers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cluster := newDockerCluster(t, ctx, dockerClusterSpec{NodeCount: 3, DataShards: 2, ParityShards: 1})
	cluster.startExplicitStorageCluster(ctx)
	if len(cluster.storagePeers) != 3 {
		t.Fatalf("storage peer count = %d, want 3", len(cluster.storagePeers))
	}
	cluster.writeTextFixture("mode-disabled/file.txt", "disabled discovery uses explicit peers\n")
	cluster.initClient(ctx, cluster.storagePeers)
	cluster.repoInit(ctx)
	cluster.backupNow(ctx)
	cluster.assertKnownPeersAtLeast(3)
}

func TestDockerRemoteDiscoveryBackupRestore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cluster := newDockerCluster(t, ctx, dockerClusterSpec{NodeCount: 3, DataShards: 2, ParityShards: 1})
	cluster.startRemoteDiscoveryNodes(ctx)
	cluster.initClient(ctx, nil)
	updateConfig(t, filepath.Join(cluster.clientDir, "config.json"), func(cfg map[string]any) {
		nodeConfig, err := os.ReadFile(filepath.Join(cluster.configDir, "node1.json"))
		if err != nil {
			t.Fatal(err)
		}
		var n map[string]any
		if err = json.Unmarshal(nodeConfig, &n); err != nil {
			t.Fatal(err)
		}
		network := cfg["network"].(map[string]any)
		network["public_discovery_peers"] = n["network"].(map[string]any)["public_discovery_peers"]
		network["discovery"] = "remote"
		network["min_peers"] = 3
		backup := cfg["backup"].(map[string]any)
		backup["data_shards"] = 2
		backup["parity_shards"] = 1
	})
	cluster.writeTextFixture("remote/file.txt", "discovered peers protect this file\n")
	cluster.repoInit(ctx)
	cluster.backupNow(ctx)
	cluster.restoreLatest(ctx, "/work/restore")
	cluster.assertRestoredBytes("file.txt", []byte("discovered peers protect this file\n"))
	cluster.assertClientReceiptsAtLeast(3)
	if err := filepath.WalkDir(filepath.Join(cluster.nodeDataDir("bootstrap"), "store"), func(_ string, d os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".shard") {
			t.Error("discovery infrastructure stored a shard")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
