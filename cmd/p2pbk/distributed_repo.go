package main

import (
	"context"
	"fmt"
	"os"

	"p2pbk/internal/config"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/node"
	"p2pbk/internal/trust/identity"
)

type repositoryHandle struct {
	repo  localRepository
	close func() error
}

func openRepositoryForCommand(ctx context.Context, cfg config.Config) (repositoryHandle, error) {
	if len(cfg.Network.BootstrapPeers) == 0 {
		repo, err := openLocalRepository(cfg)
		return repositoryHandle{repo: repo}, err
	}

	paths, err := cfg.Paths()
	if err != nil {
		return repositoryHandle{}, err
	}
	passphrase := cfg.Trust.IdentityPassphrase
	if passphrase == "" {
		passphrase = os.Getenv("P2PBK_IDENTITY_PASSPHRASE")
	}
	if passphrase == "" {
		return repositoryHandle{}, fmt.Errorf("identity passphrase is required for distributed mode; set trust.identity_passphrase or P2PBK_IDENTITY_PASSPHRASE")
	}

	id, err := identity.Load(paths.Identity, passphrase)
	if err != nil {
		return repositoryHandle{}, fmt.Errorf("load identity: %w", err)
	}
	h, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: cfg.Network.ListenAddrs})
	if err != nil {
		return repositoryHandle{}, err
	}

	distributed, err := node.OpenDistributedStorage(ctx, cfg, h, id)
	if err != nil {
		_ = h.Close()
		return repositoryHandle{}, err
	}
	repo, err := openStorageRepository(cfg, distributed.Storage)
	if err != nil {
		_ = distributed.Close(ctx)
		_ = h.Close()
		return repositoryHandle{}, err
	}

	return repositoryHandle{
		repo: repo,
		close: func() error {
			return errorsJoin(distributed.Close(ctx), h.Close())
		},
	}, nil
}
