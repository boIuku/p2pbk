package host

import (
	"context"
	"testing"
)

func TestNewCreatesListeningHost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := New(ctx, Options{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	defer h.Close()

	if h.ID() == "" {
		t.Fatal("host has empty peer id")
	}
	if len(h.Addrs()) == 0 {
		t.Fatal("host has no listen addresses")
	}
}

func TestConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := New(ctx, Options{})
	if err != nil {
		t.Fatalf("New a returned error: %v", err)
	}
	defer a.Close()

	b, err := New(ctx, Options{})
	if err != nil {
		t.Fatalf("New b returned error: %v", err)
	}
	defer b.Close()

	if err := Connect(ctx, a, AddrInfo(b)); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
}
