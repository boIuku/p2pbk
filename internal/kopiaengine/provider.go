package kopiaengine

import (
	"context"
	"fmt"
	"os"
	"sync"

	kopiablob "github.com/kopia/kopia/repo/blob"

	"p2pbk/internal/config"
	"p2pbk/internal/node"
	"p2pbk/internal/trust/identity"
)

var registerOnce sync.Once

func RegisterProvider() {
	registerOnce.Do(func() {
		kopiablob.AddSupportedStorage(storageType, StorageOptions{}, newStorage)
	})
}

func newStorage(ctx context.Context, opt *StorageOptions, isCreate bool) (kopiablob.Storage, error) {
	if opt == nil || opt.ConfigPath == "" {
		return nil, fmt.Errorf("p2pbk storage configPath is required")
	}
	cfg, err := config.Load(opt.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load p2pbk config: %w", err)
	}

	var id *identity.Identity
	distributed, err := node.ShouldUseDistributedStorage(cfg)
	if err != nil {
		return nil, err
	}
	if distributed {
		paths, err := cfg.Paths()
		if err != nil {
			return nil, err
		}
		passphrase := cfg.Trust.IdentityPassphrase
		if passphrase == "" {
			passphrase = os.Getenv("P2PBK_IDENTITY_PASSPHRASE")
		}
		if passphrase == "" {
			return nil, fmt.Errorf("identity passphrase is required for distributed p2pbk storage")
		}
		id, err = identity.Load(paths.Identity, passphrase)
		if err != nil {
			return nil, fmt.Errorf("load p2pbk identity: %w", err)
		}
	}

	backend, err := node.OpenBlobStorage(ctx, cfg, id)
	if err != nil {
		return nil, err
	}
	return &openedStorage{
		Storage: backend.Storage,
		options: *opt,
		backend: backend,
	}, nil
}
