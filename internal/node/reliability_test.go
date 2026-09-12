package node

import (
	"context"
	"os"
	p2phost "p2pbk/internal/network/host"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kopia/kopia/repo/blob"
	"p2pbk/internal/config"
)

func isolatedConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Node.DataDir = t.TempDir()
	cfg.Storage.StoreDir = "store"
	cfg.Storage.CacheDir = "cache"
	cfg.Network.Discovery = "disabled"
	return cfg
}

func TestRemoteBackupRequiresWritableStorageCapability(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	infra, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer infra.Close()
	cfg := isolatedConfig(t)
	cfg.Network.BootstrapPeers = []string{firstP2PAddr(infra)}
	cfg.Network.MinPeers = 1
	st, _, err := OpenDistributedStorageWithLocator(ctx, cfg, client, newTestLocator())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(ctx)
	err = st.PutBlob(ctx, "blob", byteBlob("must be remote"), blob.PutOptions{})
	if err == nil || !strings.Contains(err.Error(), "insufficient writable storage peers") {
		t.Fatalf("connected non-storage peer counted as protection: %v", err)
	}
	var listed int
	_ = st.ListBlobs(ctx, "", func(blob.Metadata) error { listed++; return nil })
	if listed != 0 {
		t.Fatal("failed remote upload published a blob")
	}
}

// P0-04: peer book changes must never reinterpret the storage of existing blobs.
func TestRepositoryStorageModeIsStable(t *testing.T) {
	ctx := context.Background()
	cfg := isolatedConfig(t)
	st, err := OpenBlobStorage(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Storage.PutBlob(ctx, "blob", byteBlob("local data"), blob.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	st.Close(ctx)
	cfg.Network.BootstrapPeers = []string{"a-newly-discovered-peer"}
	remote, err := ShouldUseDistributedStorage(cfg)
	if err != nil || remote {
		t.Fatalf("peer discovery switched local repository: %t %v", remote, err)
	}
	reopened, err := OpenBlobStorage(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(ctx)
	var out outputBuffer
	if err = reopened.Storage.GetBlob(ctx, "blob", 0, -1, &out); err != nil || string(out.bytes) != "local data" {
		t.Fatalf("old backup became inaccessible: %q %v", out.bytes, err)
	}
	cfg.Storage.Mode = "remote"
	if _, err = ShouldUseDistributedStorage(cfg); err == nil {
		t.Fatal("implicit local-to-remote migration accepted")
	}
	remoteCfg := isolatedConfig(t)
	if err = pinStorageMode(remoteCfg, true); err != nil {
		t.Fatal(err)
	}
	remote, err = ShouldUseDistributedStorage(remoteCfg)
	if err != nil || !remote {
		t.Fatalf("empty peer book demoted remote repository: %t %v", remote, err)
	}
}

func TestMissingManifestCannotInitializeReplacementRepository(t *testing.T) {
	cfg := isolatedConfig(t)
	if err := pinStorageMode(cfg, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Node.DataDir, "kopia.repository.config"), []byte("existing repository"), 0600); err != nil {
		t.Fatal(err)
	}
	if st, err := OpenBlobStorage(context.Background(), cfg, nil); err == nil {
		st.Close(context.Background())
		t.Fatal("missing metadata silently became an empty repository")
	}
}
