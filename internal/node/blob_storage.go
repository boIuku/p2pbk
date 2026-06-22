package node

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/kopia/kopia/repo/blob"

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
	if len(cfg.Network.BootstrapPeers) == 0 {
		st, err := openLocalBlobStorage(cfg)
		if err != nil {
			return nil, err
		}
		return &BlobStorage{Storage: st, close: st.Close}, nil
	}

	h, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: cfg.Network.ListenAddrs})
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
