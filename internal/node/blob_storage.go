package node

import (
	"context"
	"errors"
	"fmt"
	"github.com/gofrs/flock"
	"os"
	"p2pbk/internal/durable"
	"path/filepath"
	"time"

	"github.com/kopia/kopia/repo/blob"
	"github.com/libp2p/go-libp2p/core/peer"

	"p2pbk/internal/backend/p2p"
	"p2pbk/internal/config"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/identity"
)

type BlobStorage struct {
	Storage blob.Storage
	close   func(context.Context) error
}

func OpenBlobStorage(ctx context.Context, cfg config.Config, id *identity.Identity) (*BlobStorage, error) {
	paths, err := cfg.Paths()
	if err != nil {
		return nil, err
	}
	useDistributed, err := ShouldUseDistributedStorage(cfg)
	if err != nil {
		return nil, err
	}
	if err := pinStorageMode(cfg, useDistributed); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(paths.DataDir, "kopia.repository.config")); err == nil {
		if _, err := os.Stat(filepath.Join(paths.DataDir, "manifests.json")); err != nil {
			return nil, fmt.Errorf("recovery metadata missing: restore a recovery bundle: %w", err)
		}
	}
	if !useDistributed {
		st, err := openLocalBlobStorage(cfg)
		if err != nil {
			return nil, err
		}
		return &BlobStorage{Storage: st, close: st.Close}, nil
	}

	relays, err := p2phost.ParseAddrInfos(cfg.Network.PublicDiscoveryPeers)
	if err != nil {
		return nil, err
	}
	h, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: []string{"/ip4/0.0.0.0/tcp/0"}, KeyPath: filepath.Join(paths.DataDir, "network.key"), StaticRelays: relays, EnableNATPortMap: true, EnableHolePunching: len(relays) > 0, ForcePrivateRelay: cfg.Network.ForcePrivateReachabilityForRelay})
	if err != nil {
		return nil, err
	}
	distributed, err := OpenDistributedStorage(ctx, cfg, h, id)
	if err != nil {
		_ = h.Close()
		return nil, err
	}
	return &BlobStorage{
		Storage: distributed.Storage,
		close: func(ctx context.Context) error {
			return errorsJoin(distributed.Close(ctx), h.Close())
		},
	}, nil
}

func ShouldUseDistributedStorage(cfg config.Config) (bool, error) {
	paths, err := cfg.Paths()
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(filepath.Join(paths.DataDir, "storage.mode"))
	if err == nil {
		mode := string(data)
		if mode != "local" && mode != "remote" {
			return false, errors.New("invalid persisted storage mode")
		}
		if cfg.Storage.Mode != "" && cfg.Storage.Mode != "auto" && cfg.Storage.Mode != mode {
			return false, errors.New("storage mode change requires explicit migration")
		}
		return mode == "remote", nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if _, err := os.Stat(filepath.Join(paths.DataDir, "manifests.json")); err == nil {
		return false, errors.New("repository storage mode missing: restore recovery metadata; legacy repositories require migration")
	}
	if cfg.Storage.Mode == "local" {
		return false, nil
	}
	if cfg.Storage.Mode == "remote" || len(cfg.Network.PublicDiscoveryPeers) > 0 {
		return true, nil
	}
	if len(cfg.Network.BootstrapPeers) > 0 {
		return true, nil
	}
	store, closeStore, err := openPeerStore(cfg)
	if err != nil {
		return false, err
	}
	defer closeStore()
	peers, err := store.ListPeers()
	if err != nil {
		return false, err
	}
	filter, err := publicDiscoveryFilter(cfg)
	if err != nil {
		return false, err
	}
	for _, record := range peers {
		id, err := peerID(record.ID)
		if err == nil && !filter.Ignored(id) {
			return true, nil
		}
	}
	return false, nil
}

func pinStorageMode(cfg config.Config, remote bool) error {
	paths, err := cfg.Paths()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(paths.DataDir, 0700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(paths.DataDir, "storage.mode.lock"))
	ok, err := lock.TryLockContext(context.Background(), 10*time.Millisecond)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("storage mode locked")
	}
	defer lock.Unlock()
	mode := "local"
	if remote {
		mode = "remote"
	}
	path := filepath.Join(paths.DataDir, "storage.mode")
	if data, err := os.ReadFile(path); err == nil {
		if string(data) != mode {
			return errors.New("storage mode changed concurrently")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return durable.WriteFile(path, []byte(mode), 0600)
}

func peerID(value string) (peer.ID, error) {
	return peer.Decode(value)
}

func (b *BlobStorage) Close(ctx context.Context) error {
	if b == nil || b.close == nil {
		return nil
	}
	return b.close(ctx)
}

func openLocalBlobStorage(cfg config.Config) (*p2p.Storage, error) {
	paths, err := cfg.Paths()
	if err != nil {
		return nil, err
	}
	server, err := blobserver.New(paths.StoreDir)
	if err != nil {
		return nil, err
	}
	return p2p.NewStorage(server, p2p.Options{
		Owner:        owner(cfg),
		DataShards:   cfg.Backup.DataShards,
		ParityShards: cfg.Backup.ParityShards,
		DisplayName:  "p2pbk local storage",
		ManifestPath: filepath.Join(paths.DataDir, "manifests.json"),
	})
}

func errorsJoin(errs ...error) error {
	var out error
	for _, err := range errs {
		if err == nil {
			continue
		}
		if out == nil {
			out = err
		} else {
			out = errors.Join(out, err)
		}
	}
	return out
}
