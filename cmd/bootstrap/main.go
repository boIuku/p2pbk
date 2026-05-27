package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"p2pbk/internal/config"
	p2pdht "p2pbk/internal/network/dht"
	p2phost "p2pbk/internal/network/host"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	host, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: cfg.Network.ListenAddrs})
	if err != nil {
		return err
	}
	defer host.Close()

	dht, err := p2pdht.New(ctx, host, p2pdht.ModeServer)
	if err != nil {
		return err
	}
	defer dht.Close()
	if err := dht.Bootstrap(ctx); err != nil {
		return err
	}

	fmt.Printf("bootstrap peer: %s\n", host.ID())
	for _, addr := range host.Addrs() {
		fmt.Printf("%s/p2p/%s\n", addr, host.ID())
	}

	<-ctx.Done()
	return nil
}
