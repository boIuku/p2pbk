package dht

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	kaddht "github.com/libp2p/go-libp2p-kad-dht"
	corediscovery "github.com/libp2p/go-libp2p/core/discovery"
	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	routingdiscovery "github.com/libp2p/go-libp2p/p2p/discovery/routing"
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
	options := []kaddht.Option{
		kaddht.ProtocolPrefix("/p2pbk"),
		kaddht.NamespacedValidator("p2pbk", p2pbkValidator{}),
	}
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

type p2pbkValidator struct{}

func (p2pbkValidator) Validate(key string, value []byte) error {
	if key == "" {
		return errors.New("dht key is required")
	}
	if len(value) == 0 {
		return errors.New("dht value is required")
	}
	return nil
}

func (p2pbkValidator) Select(key string, values [][]byte) (int, error) {
	if len(values) == 0 {
		return 0, errors.New("cannot select from no dht values")
	}
	best := 0
	for i := 1; i < len(values); i++ {
		if dhtRecordTime(values[i]).After(dhtRecordTime(values[best])) {
			best = i
		}
	}
	return best, nil
}

func dhtRecordTime(value []byte) time.Time {
	var record struct {
		IssuedAt time.Time `json:"issued_at"`
	}
	if err := json.Unmarshal(value, &record); err != nil {
		return time.Time{}
	}
	return record.IssuedAt
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

func (d *DHT) Advertise(ctx context.Context, namespace string, ttl time.Duration) (time.Duration, error) {
	opts := []corediscovery.Option{}
	if ttl > 0 {
		opts = append(opts, corediscovery.TTL(ttl))
	}
	advertisedTTL, err := routingdiscovery.NewRoutingDiscovery(d.inner).Advertise(ctx, namespace, opts...)
	if err != nil {
		return 0, fmt.Errorf("advertise discovery namespace %q: %w", namespace, err)
	}
	return advertisedTTL, nil
}

func (d *DHT) FindPeers(ctx context.Context, namespace string, limit int) (<-chan peer.AddrInfo, error) {
	opts := []corediscovery.Option{}
	if limit > 0 {
		opts = append(opts, corediscovery.Limit(limit))
	}
	peers, err := routingdiscovery.NewRoutingDiscovery(d.inner).FindPeers(ctx, namespace, opts...)
	if err != nil {
		return nil, fmt.Errorf("find peers in discovery namespace %q: %w", namespace, err)
	}
	return peers, nil
}

func (d *DHT) Close() error {
	return d.inner.Close()
}
