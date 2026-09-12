package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// P0-01: addresses and receipts refer to an identity that survives the process.
func TestPeerIdentitySurvivesRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "network.key")
	a, err := New(ctx, Options{KeyPath: path})
	if err != nil {
		t.Fatal(err)
	}
	id := a.ID()
	a.Close()
	b, err := New(ctx, Options{KeyPath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.ID() != id {
		t.Fatalf("restart changed identity: %s -> %s", id, b.ID())
	}
	c, err := New(ctx, Options{KeyPath: filepath.Join(t.TempDir(), "network.key")})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.ID() == id {
		t.Fatal("independent nodes share an identity")
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if h, err := New(ctx, Options{KeyPath: path}); err == nil {
		h.Close()
		t.Fatal("corrupt identity was silently replaced")
	}
}
