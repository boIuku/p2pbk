package main

import (
	"context"
	"p2pbk/internal/config"
	"testing"
)

func TestDaemonPeerIdentitySurvivesRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := config.Default()
	cfg.Node.DataDir = t.TempDir()
	cfg.Storage.StoreDir = "store"
	cfg.Storage.CacheDir = "cache"
	cfg.Network.ListenAddrs = []string{"/ip4/127.0.0.1/tcp/0"}
	a, err := openDaemonRuntime(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	id := a.host.ID()
	a.Close()
	b, err := openDaemonRuntime(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.host.ID() != id {
		t.Fatal("daemon restart changed peer identity")
	}
}
