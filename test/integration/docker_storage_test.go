//go:build docker

package integration

import (
	"context"
	"testing"
	"time"
)

func TestDockerDistributedStorageWritesReceiptsAndShardFiles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cluster := newDockerCluster(t, ctx, dockerClusterSpec{NodeCount: 3, DataShards: 2, ParityShards: 1})
	cluster.startExplicitStorageCluster(ctx)
	cluster.writeBinaryFixture("storage/blob.bin", deterministicBytes(64*1024))
	cluster.initClient(ctx, cluster.storagePeers)
	cluster.repoInit(ctx)
	cluster.backupNow(ctx)

	cluster.assertClientReceiptsAtLeast(1)
	cluster.assertStoredShardFilesAtLeast(1)
}
