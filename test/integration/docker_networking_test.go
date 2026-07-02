package integration

import (
	"context"
	"testing"
	"time"
)

func TestDockerClientReconnectsSavedPeersFromLocalDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cluster := newDockerCluster(t, ctx, dockerClusterSpec{NodeCount: 3, DataShards: 2, ParityShards: 1})
	cluster.startExplicitStorageCluster(ctx)
	want := []byte("saved peers should be enough for a later restore\n")
	cluster.writeBinaryFixture("saved-peers/file.txt", want)
	cluster.initClient(ctx, cluster.storagePeers)
	cluster.repoInit(ctx)
	cluster.backupNow(ctx)
	cluster.assertKnownPeersAtLeast(3)

	cluster.setClientBootstrapPeers(nil)
	cluster.restoreLatest(ctx, "/work/restore")
	cluster.assertRestoredBytes("file.txt", want)
}
