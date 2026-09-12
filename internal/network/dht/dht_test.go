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

func TestP2PBKValidator(t *testing.T) {
	v := p2pbkValidator{}
	if err := v.Validate("/p2pbk/locator/blob", []byte("record")); err == nil {
		t.Fatal("validator accepted an unsigned arbitrary record")
	}
	if err := v.Validate("/p2pbk/locator/blob", nil); err == nil {
		t.Fatal("Validate returned nil error for empty value")
	}

	older := []byte(`{"issued_at":"2026-06-23T10:00:00Z","locations":[1,2,3]}`)
	newer := []byte(`{"issued_at":"2026-06-23T10:00:01Z","locations":[]}`)
	if _, err := v.Select("/p2pbk/locator/blob", [][]byte{older, newer}); err == nil {
		t.Fatal("unverified timestamps must not win DHT selection")
	}
}
