//go:build docker

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDockerBackupCreatesSnapshotReceiptsAndRemoteShards(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cluster := newDockerCluster(t, ctx, dockerClusterSpec{NodeCount: 3, DataShards: 2, ParityShards: 1})
	cluster.startExplicitStorageCluster(ctx)
	cluster.writeTextFixture("nested/file.txt", "hello from docker p2pbk integration\n")
	cluster.initClient(ctx, cluster.storagePeers)
	cluster.repoInit(ctx)
	cluster.backupNow(ctx)

	out, err := cluster.runClient(ctx, "snapshot", "list")
	if err != nil {
		t.Fatalf("snapshot list failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "fixtures") && strings.TrimSpace(out) == "" {
		t.Fatalf("snapshot list output did not show a snapshot:\n%s", out)
	}
	cluster.assertClientReceiptsAtLeast(1)
	cluster.assertKnownPeersAtLeast(3)
	cluster.assertStoredShardFilesAtLeast(1)
}
