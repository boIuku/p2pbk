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

func TestParseAddrInfo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := New(ctx, Options{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	defer h.Close()

	addr := h.Addrs()[0].String() + "/p2p/" + h.ID().String()
	info, err := ParseAddrInfo(addr)
	if err != nil {
		t.Fatalf("ParseAddrInfo returned error: %v", err)
	}
	if info.ID != h.ID() {
		t.Fatalf("parsed peer = %s, want %s", info.ID, h.ID())
	}
	if len(info.Addrs) == 0 {
		t.Fatal("parsed peer has no addresses")
	}
}

func TestConnectAddrs(t *testing.T) {
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

	addr := b.Addrs()[0].String() + "/p2p/" + b.ID().String()
	connected, err := ConnectAddrs(ctx, a, []string{addr})
	if err != nil {
		t.Fatalf("ConnectAddrs returned error: %v", err)
	}
	if len(connected) != 1 || connected[0].ID != b.ID() {
		t.Fatalf("connected = %+v, want peer %s", connected, b.ID())
	}
}
