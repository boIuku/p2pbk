package integration

import (
	"context"
	"testing"
	"time"
)

func TestDockerRestoreLatest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cluster := newDockerCluster(t, ctx, dockerClusterSpec{NodeCount: 3, DataShards: 2, ParityShards: 1})
	cluster.startExplicitStorageCluster(ctx)
	want := []byte("hello from docker p2pbk integration\n")
	cluster.writeBinaryFixture("nested/file.txt", want)
	cluster.initClient(ctx, cluster.storagePeers)
	cluster.repoInit(ctx)
	cluster.backupNow(ctx)
	cluster.restoreLatest(ctx, "/work/restore")
	cluster.assertRestoredBytes("file.txt", want)
}

func TestDockerRestoreAfterMaxNodeLoss(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cluster := newDockerCluster(t, ctx, dockerClusterSpec{NodeCount: 3, DataShards: 2, ParityShards: 1})
	cluster.startExplicitStorageCluster(ctx)
	want := deterministicBytes(64 * 1024)
	cluster.writeBinaryFixture("resilience/degraded-restore.bin", want)
	cluster.initClient(ctx, cluster.storagePeers)
	cluster.repoInit(ctx)
	cluster.backupNow(ctx)

	if out, err := cluster.docker.run(ctx, "stop", "node3"); err != nil {
		t.Fatalf("stop node3 failed: %v\n%s", err, out)
	}
	cluster.setClientBootstrapPeers(cluster.storagePeers[:2])
	cluster.restoreLatest(ctx, "/work/restore")
	cluster.assertRestoredBytes("degraded-restore.bin", want)
}

func TestDockerRestoreFailsBeyondParity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cluster := newDockerCluster(t, ctx, dockerClusterSpec{NodeCount: 3, DataShards: 2, ParityShards: 1})
	cluster.startExplicitStorageCluster(ctx)
	cluster.writeBinaryFixture("resilience/unrecoverable.bin", deterministicBytes(64*1024))
	cluster.initClient(ctx, cluster.storagePeers)
	cluster.repoInit(ctx)
	cluster.backupNow(ctx)

	if out, err := cluster.docker.run(ctx, "stop", "node2", "node3"); err != nil {
		t.Fatalf("stop node2/node3 failed: %v\n%s", err, out)
	}
	cluster.setClientBootstrapPeers(cluster.storagePeers[:1])
	if out, err := cluster.runClient(ctx, "restore", "latest", "/work/restore"); err == nil {
		t.Fatalf("restore unexpectedly succeeded after losing more nodes than parity allows:\n%s", out)
	} else {
		t.Logf("restore failed as expected after excessive node loss:\n%s", out)
	}
}
