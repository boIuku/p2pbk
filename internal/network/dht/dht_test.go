package dht

import (
	"context"
	"testing"

	p2phost "p2pbk/internal/network/host"
)

func TestNewRejectsUnsupportedMode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("host New returned error: %v", err)
	}
	defer h.Close()

	if _, err := New(ctx, h, Mode("other")); err == nil {
		t.Fatal("New returned nil error for unsupported mode")
	}
}

func TestNewCreatesClientDHT(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("host New returned error: %v", err)
	}
	defer h.Close()

	d, err := New(ctx, h, ModeClient)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	defer d.Close()
}
