package kopiaengine

import (
	"context"
	"fmt"
	"strings"
	"sync"

	kopiablob "github.com/kopia/kopia/repo/blob"

	"p2pbk/internal/config"
	"p2pbk/internal/node"
	"p2pbk/internal/secrets"
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
			passphrase = secrets.FromEnv()
		}
		if passphrase == "" {
			return nil, fmt.Errorf("password is required for distributed p2pbk storage")
		}
		id, err = identity.Load(paths.Identity, passphrase)
		if err != nil {
			return nil, identityLoadError(err)
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

func identityLoadError(err error) error {
	if strings.Contains(err.Error(), "no identity matched any of the recipients") {
		return fmt.Errorf("open p2pbk identity: wrong password")
	}
	return fmt.Errorf("open p2pbk identity: %w", err)
}
