package wire

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	p2phost "p2pbk/internal/network/host"
)

func wirePair(t *testing.T) (corehost.Host, corehost.Host) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	b, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	if err := a.Connect(ctx, p2phost.AddrInfo(b)); err != nil {
		t.Fatal(err)
	}
	return a, b
}

// P0-05: hostile framing cannot allocate without bound or occupy a handler forever.
func TestRPCRejectsOversizedMessage(t *testing.T) {
	a, b := wirePair(t)
	r := NewRouter()
	r.MaxBytes = 1024
	r.Timeout = 200 * time.Millisecond
	called := make(chan struct{}, 1)
	r.Handle("oversize", func(context.Context, Request) (Response, error) { called <- struct{}{}; return OK(nil) })
	r.Register(b)
	s, err := a.NewStream(context.Background(), b.ID(), ProtocolID)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetDeadline(time.Now().Add(time.Second))
	// Bypass Client's outbound guard to exercise the remote decoder itself.
	_, _ = s.Write([]byte(`{"method":"oversize","payload":"` + strings.Repeat("x", 2048) + `"}` + "\n"))
	var resp Response
	if err := json.NewDecoder(s).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.OK || !strings.Contains(resp.Error, "exceeds limit") {
		t.Fatalf("oversized request: %+v", resp)
	}
	select {
	case <-called:
		t.Fatal("oversized payload reached application handler")
	default:
	}
}

func TestRPCServerBoundsUnfinishedRequest(t *testing.T) {
	a, b := wirePair(t)
	r := NewRouter()
	r.Timeout = 50 * time.Millisecond
	r.Register(b)
	s, err := a.NewStream(context.Background(), b.ID(), ProtocolID)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetDeadline(time.Now().Add(time.Second))
	if _, err = s.Write([]byte(`{"method":`)); err != nil {
		t.Fatal(err)
	}
	// The server deadline must terminate an unfinished request, not the client's deadline.
	started := time.Now()
	var resp Response
	_ = json.NewDecoder(s).Decode(&resp)
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("server retained an unfinished request until client deadline")
	}
}

// P0-05: cancelling a request must also interrupt I/O AFTER protocol negotiation.
// A peer that accepts a stream is not evidence that it will ever send a response.
func TestRPCCancelAfterStreamOpened(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "silent", true: "partial_json"}[partial], func(t *testing.T) {
			a, b := wirePair(t)
			opened := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			b.SetStreamHandler(ProtocolID, func(s network.Stream) {
				defer s.Close()
				var req Request
				if err := json.NewDecoder(s).Decode(&req); err != nil {
					return
				}
				if partial {
					s.Write([]byte(`{"ok":`))
				}
				close(opened)
				<-release
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- NewClient(a).Call(ctx, b.ID(), "stall", nil, nil) }()
			select {
			case <-opened:
			case <-time.After(3 * time.Second):
				t.Fatal("stream never opened")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("got %v, want cancellation", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled RPC is still blocked reading a peer")
			}
		})
	}
}
