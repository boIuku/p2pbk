package wire

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	p2phost "p2pbk/internal/network/host"
)

func TestClientCallsRemoteHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("host a New returned error: %v", err)
	}
	defer a.Close()

	b, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("host b New returned error: %v", err)
	}
	defer b.Close()

	router := NewRouter()
	router.Handle("ping", func(ctx context.Context, req Request) (Response, error) {
		var in struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(req.Payload, &in); err != nil {
			return Response{}, err
		}
		return OK(struct {
			Message string `json:"message"`
		}{Message: "pong:" + in.Message})
	})
	router.Register(b)

	if err := p2phost.Connect(ctx, a, p2phost.AddrInfo(b)); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	var out struct {
		Message string `json:"message"`
	}
	err = NewClient(a).Call(ctx, b.ID(), "ping", map[string]string{"message": "hello"}, &out)
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if out.Message != "pong:hello" {
		t.Fatalf("response message = %q", out.Message)
	}
}

func TestClientReturnsRemoteErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("host a New returned error: %v", err)
	}
	defer a.Close()

	b, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatalf("host b New returned error: %v", err)
	}
	defer b.Close()

	router := NewRouter()
	router.Handle("fail", func(ctx context.Context, req Request) (Response, error) {
		return Response{}, errors.New("boom")
	})
	router.Register(b)

	if err := p2phost.Connect(ctx, a, p2phost.AddrInfo(b)); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	err = NewClient(a).Call(ctx, b.ID(), "fail", nil, nil)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("Call error = %v, want boom", err)
	}
}
