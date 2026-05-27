package dht

import (
	"context"
	"fmt"

	kaddht "github.com/libp2p/go-libp2p-kad-dht"
	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Mode string

const (
	ModeClient Mode = "client"
	ModeServer Mode = "server"
)

type DHT struct {
	inner *kaddht.IpfsDHT
}

func New(ctx context.Context, h corehost.Host, mode Mode) (*DHT, error) {
	options := []kaddht.Option{}
	switch mode {
	case "", ModeClient:
		options = append(options, kaddht.Mode(kaddht.ModeClient))
	case ModeServer:
		options = append(options, kaddht.Mode(kaddht.ModeServer))
	default:
		return nil, fmt.Errorf("unsupported dht mode %q", mode)
	}

	inner, err := kaddht.New(ctx, h, options...)
	if err != nil {
		return nil, fmt.Errorf("create dht: %w", err)
	}
	return &DHT{inner: inner}, nil
}

func (d *DHT) Bootstrap(ctx context.Context) error {
	if err := d.inner.Bootstrap(ctx); err != nil {
		return fmt.Errorf("bootstrap dht: %w", err)
	}
	return nil
}

func (d *DHT) FindPeer(ctx context.Context, id peer.ID) (peer.AddrInfo, error) {
	info, err := d.inner.FindPeer(ctx, id)
	if err != nil {
		return peer.AddrInfo{}, fmt.Errorf("find peer %s: %w", id, err)
	}
	return info, nil
}

func (d *DHT) PutValue(ctx context.Context, key string, value []byte) error {
	if err := d.inner.PutValue(ctx, key, value); err != nil {
		return fmt.Errorf("put dht value %q: %w", key, err)
	}
	return nil
}

func (d *DHT) GetValue(ctx context.Context, key string) ([]byte, error) {
	value, err := d.inner.GetValue(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get dht value %q: %w", key, err)
	}
	return value, nil
}

func (d *DHT) Close() error {
	return d.inner.Close()
}
