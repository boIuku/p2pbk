package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"p2pbk/internal/config"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/peer/quota"
	"p2pbk/internal/peer/responder"
	"p2pbk/internal/trust/reputation"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	configPath := ""
	if len(args) > 0 {
		configPath = args[0]
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	paths, err := cfg.Paths()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	host, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: cfg.Network.ListenAddrs})
	if err != nil {
		return err
	}
	defer host.Close()

	db, err := localdb.Open(paths.LocalDB)
	if err != nil {
		return err
	}
	defer db.Close()

	q, err := quota.New(cfg.Storage.OfferBytes)
	if err != nil {
		return err
	}
	blobServer, err := blobserver.NewWithQuota(paths.StoreDir, q)
	if err != nil {
		return err
	}

	router := wire.NewRouter()
	blobServer.Register(router)
	reputationWindow, _ := time.ParseDuration(cfg.Trust.ReputationWindow)
	responder.New(blobServer, db, reputation.New(reputationWindow), responder.WithPeerID(host.ID())).Register(router)
	router.Register(host)

	fmt.Printf("backupd peer: %s\n", host.ID())
	for _, addr := range host.Addrs() {
		fmt.Printf("listening: %s/p2p/%s\n", addr, host.ID())
	}
	fmt.Printf("store: %s\n", filepath.Clean(paths.StoreDir))

	<-ctx.Done()
	return nil
}
