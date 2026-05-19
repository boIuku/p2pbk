package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const ProtocolID protocol.ID = "/p2pbk/1.0.0"

var ErrNoHandler = errors.New("no wire handler registered")

type Request struct {
	Method  string          `json:"method"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Response struct {
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type Handler func(context.Context, Request) (Response, error)

type Router struct {
	handlers map[string]Handler
}

func NewRouter() *Router {
	return &Router{handlers: map[string]Handler{}}
}

func (r *Router) Handle(method string, handler Handler) {
	r.handlers[method] = handler
}

func (r *Router) Register(h corehost.Host) {
	h.SetStreamHandler(ProtocolID, r.handleStream)
}

func (r *Router) handleStream(stream network.Stream) {
	defer stream.Close()

	var req Request
	if err := json.NewDecoder(stream).Decode(&req); err != nil {
		_ = json.NewEncoder(stream).Encode(Response{OK: false, Error: fmt.Sprintf("decode request: %v", err)})
		return
	}

	handler := r.handlers[req.Method]
	if handler == nil {
		_ = json.NewEncoder(stream).Encode(Response{OK: false, Error: fmt.Sprintf("%v: %s", ErrNoHandler, req.Method)})
		return
	}

	resp, err := handler(context.Background(), req)
	if err != nil {
		resp = Response{OK: false, Error: err.Error()}
	} else if !resp.OK && resp.Error == "" {
		resp.OK = true
	}

	_ = json.NewEncoder(stream).Encode(resp)
}

type Client struct {
	host corehost.Host
}

func NewClient(h corehost.Host) Client {
	return Client{host: h}
}

func (c Client) Call(ctx context.Context, peerID peer.ID, method string, payload any, out any) error {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	stream, err := c.host.NewStream(ctx, peerID, ProtocolID)
	if err != nil {
		return fmt.Errorf("open stream to %s: %w", peerID, err)
	}
	defer stream.Close()

	if err := json.NewEncoder(stream).Encode(Request{Method: method, Payload: rawPayload}); err != nil {
		_ = stream.Reset()
		return fmt.Errorf("encode request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("wire response closed before payload")
		}
		return fmt.Errorf("decode response: %w", err)
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	if out != nil && len(resp.Payload) > 0 {
		if err := json.Unmarshal(resp.Payload, out); err != nil {
			return fmt.Errorf("decode response payload: %w", err)
		}
	}
	return nil
}

func OK(payload any) (Response, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("encode response payload: %w", err)
	}
	return Response{OK: true, Payload: raw}, nil
}
