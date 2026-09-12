package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"p2pbk/internal/config"
	"p2pbk/internal/kopiaengine"
	p2pdht "p2pbk/internal/network/dht"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/quota"
	"p2pbk/internal/recovery"
	"p2pbk/internal/trust/identity"
)

type durablePeer struct {
	host      corehost.Host
	dht       *p2pdht.DHT
	dir, addr string
}

func (p *durablePeer) stop() {
	if p.dht != nil {
		p.dht.Close()
	}
	if p.host != nil {
		p.host.Close()
	}
}
func (p *durablePeer) start(t *testing.T, ctx context.Context) {
	t.Helper()
	opts := p2phost.Options{KeyPath: filepath.Join(p.dir, "network.key")}
	if p.addr != "" {
		opts.ListenAddrs = []string{p.addr}
	}
	h, err := p2phost.New(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	p.host = h
	p.addr = h.Addrs()[0].String()
	p.dht, err = p2pdht.New(ctx, h, p2pdht.ModeServer)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := quota.New(100 << 20)
	s, err := blobserver.NewWithQuota(filepath.Join(p.dir, "store"), q)
	if err != nil {
		t.Fatal(err)
	}
	r := wire.NewRouter()
	s.Register(r)
	r.Register(h)
}

// The oracle is an independent tree manifest, never a search for a basename.
// It detects missing/extra paths, wrong nesting, empty directories and byte changes.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			out[rel] = "dir"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = fmt.Sprintf("%d:%x", len(data), sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func assertTree(t *testing.T, root string, want map[string]string) {
	t.Helper()
	if got := tree(t, root); !reflect.DeepEqual(got, want) {
		t.Fatalf("restored tree differs\ngot: %#v\nwant:%#v", got, want)
	}
}
func fixture(t *testing.T, root string) {
	t.Helper()
	for name, data := range map[string][]byte{"one/same.txt": []byte("first version"), "two/same.txt": []byte("different path"), "unicode-ą/文件.txt": []byte("private known marker 99731"), "empty": {}, "data.bin": bytes.Repeat([]byte{0, 255, 1, 127}, 16000)} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "empty-directory"), 0700); err != nil {
		t.Fatal(err)
	}
}

// P0-01/03/08: prove a cold restore with REAL Kopia, libp2p and DHT nodes.
// No source data, cache, original database, or running old peer process may help.
func TestRestoreFromRecoveryBundle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	root := t.TempDir()
	cfg := config.Default()
	cfg.Node.DataDir = filepath.Join(root, "owner")
	cfg.Node.Identity = "identity.age"
	cfg.Node.DisplayName = "same display name"
	cfg.Storage.StoreDir = "store"
	cfg.Storage.CacheDir = "cache"
	cfg.Storage.Mode = "remote"
	cfg.Network.Discovery = "disabled"
	cfg.Network.MinPeers = 3
	cfg.Backup.DataShards = 2
	cfg.Backup.ParityShards = 1
	var peers []*durablePeer
	for i := 0; i < 3; i++ {
		p := &durablePeer{dir: filepath.Join(root, fmt.Sprintf("peer-%d", i))}
		p.start(t, ctx)
		t.Cleanup(p.stop)
		peers = append(peers, p)
		cfg.Network.BootstrapPeers = append(cfg.Network.BootstrapPeers, p.addr+"/p2p/"+p.host.ID().String())
	}
	if _, err := identity.Create(filepath.Join(cfg.Node.DataDir, "identity.age"), "pass"); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(cfg.Node.DataDir, "config.json")
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	fixture(t, source)
	wantA := tree(t, source)
	engine := kopiaengine.New(configPath, cfg, "pass")
	a, err := engine.Backup(ctx, source, "A")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "one", "same.txt"), []byte("second version with a change"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, "two", "same.txt")); err != nil {
		t.Fatal(err)
	}
	wantB := tree(t, source)
	b, err := engine.Backup(ctx, source, "B")
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(root, "offsite.age")
	if err := recovery.Export(ctx, cfg, "pass", bundlePath); err != nil {
		t.Fatal(err)
	}
	encrypted, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("private_key")) {
		t.Fatal("bundle exposes private keys")
	}
	if _, err := recovery.Import(ctx, bundlePath, "wrong", filepath.Join(root, "wrong-password")); err == nil {
		t.Fatal("wrong password unlocked recovery")
	}
	if _, err := os.Stat(filepath.Join(root, "wrong-password")); !os.IsNotExist(err) {
		t.Fatal("failed import modified target")
	}
	// These paths are generated children of t.TempDir, never user data.
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(cfg.Node.DataDir); err != nil {
		t.Fatal(err)
	}
	for _, p := range peers {
		old := p.host.ID()
		p.stop()
		p.start(t, ctx)
		if p.host.ID() != old {
			t.Fatal("storage restart changed peer ID")
		}
	}
	// All DHT records were in the previous processes. The bundle must suffice.
	recoveredConfig, err := recovery.Import(ctx, bundlePath, "pass", filepath.Join(root, "replacement"))
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := config.Load(recoveredConfig)
	if err != nil {
		t.Fatal(err)
	}
	engine = kopiaengine.New(recoveredConfig, recovered, "pass")
	if err = engine.EnsureRepository(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		want map[string]string
	}{{string(a.ManifestID), wantA}, {string(b.ManifestID), wantB}} {
		target := filepath.Join(root, "restore-"+tc.id)
		if _, err = engine.Restore(ctx, tc.id, target); err != nil {
			t.Fatal(err)
		}
		assertTree(t, target, tc.want)
	}
	// P0-08: each possible single-peer outage, unchanged min_peers and bootstrap list.
	for i, p := range peers {
		t.Run(fmt.Sprintf("one_peer_lost_%d", i), func(t *testing.T) {
			p.stop()
			defer p.start(t, ctx)
			// Reimport into a new directory so no Kopia cache can mask shard retrieval.
			path, err := recovery.Import(ctx, bundlePath, "pass", filepath.Join(root, fmt.Sprintf("degraded-%d", i)))
			if err != nil {
				t.Fatal(err)
			}
			c, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			e := kopiaengine.New(path, c, "pass")
			if err = e.EnsureRepository(ctx); err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(root, fmt.Sprintf("out-%d", i))
			if _, err = e.Restore(ctx, string(b.ManifestID), dst); err != nil {
				t.Fatal(err)
			}
			assertTree(t, dst, wantB)
		})
	}
	peers[0].stop()
	peers[1].stop()
	path, err := recovery.Import(ctx, bundlePath, "pass", filepath.Join(root, "beyond-parity"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	err = kopiaengine.New(path, c, "pass").EnsureRepository(ctx)
	if err == nil || !strings.Contains(err.Error(), "not enough valid shards") || ctx.Err() != nil {
		t.Fatalf("want insufficient-shards failure before deadline, got %v (ctx=%v)", err, ctx.Err())
	}
}
