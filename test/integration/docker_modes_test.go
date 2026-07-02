package integration

import (
	"context"
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

func TestDockerRemoteDiscoveryModeStarts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cluster := newDockerCluster(t, ctx, dockerClusterSpec{NodeCount: 2, DataShards: 2, ParityShards: 1})
	cluster.startRemoteDiscoveryNodes(ctx)
}
