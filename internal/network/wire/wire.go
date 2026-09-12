package wire

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/libp2p/go-libp2p/core/crypto"
	"io"
	"time"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const ProtocolID protocol.ID = "/p2pbk/1.0.0"

var ErrNoHandler = errors.New("no wire handler registered")

const MaxMessageBytes = 64 << 20
const DefaultTimeout = 30 * time.Second

var ErrMessageTooLarge = errors.New("wire message exceeds limit")

type requestIdentity struct{}
type localIdentity struct{}

func RemotePeer(ctx context.Context) peer.ID {
	id, _ := ctx.Value(requestIdentity{}).(peer.ID)
	return id
}
func LocalKey(ctx context.Context) crypto.PrivKey {
	key, _ := ctx.Value(localIdentity{}).(crypto.PrivKey)
	return key
}

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
	MaxBytes int
	Timeout  time.Duration
}

func NewRouter() *Router {
	return &Router{handlers: map[string]Handler{}, MaxBytes: MaxMessageBytes, Timeout: DefaultTimeout}
}

func (r *Router) Handle(method string, handler Handler) {
	r.handlers[method] = handler
}

func (r *Router) Register(h corehost.Host) {
	h.SetStreamHandler(ProtocolID, func(s network.Stream) { r.handleStreamWithKey(s, h.Peerstore().PrivKey(h.ID())) })
}

func (r *Router) handleStream(stream network.Stream) {
	r.handleStreamWithKey(stream, nil)
}

func (r *Router) handleStreamWithKey(stream network.Stream, key crypto.PrivKey) {
	defer stream.Close()
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = stream.SetDeadline(time.Now().Add(timeout))
	ctx = context.WithValue(ctx, requestIdentity{}, stream.Conn().RemotePeer())
	if key != nil {
		ctx = context.WithValue(ctx, localIdentity{}, key)
	}
	max := r.MaxBytes
	if max <= 0 {
		max = MaxMessageBytes
	}

	var req Request
	if err := readMessage(stream, max, &req); err != nil {
		_ = json.NewEncoder(stream).Encode(Response{OK: false, Error: fmt.Sprintf("decode request: %v", err)})
		return
	}

	handler := r.handlers[req.Method]
	if handler == nil {
		_ = json.NewEncoder(stream).Encode(Response{OK: false, Error: fmt.Sprintf("%v: %s", ErrNoHandler, req.Method)})
		return
	}

	resp, err := handler(ctx, req)
	if err != nil {
		resp = Response{OK: false, Error: err.Error()}
	} else if !resp.OK && resp.Error == "" {
		resp.OK = true
	}

	_ = writeMessage(stream, max, resp)
}

type Client struct {
	host     corehost.Host
	MaxBytes int
	Timeout  time.Duration
}

func NewClient(h corehost.Host) Client {
	return Client{host: h, MaxBytes: MaxMessageBytes, Timeout: DefaultTimeout}
}

func (c Client) Call(ctx context.Context, peerID peer.ID, method string, payload any, out any) error {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	max := c.MaxBytes
	if max <= 0 {
		max = MaxMessageBytes
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	if len(rawPayload) > max {
		return ErrMessageTooLarge
	}

	stream, err := c.host.NewStream(ctx, peerID, ProtocolID)
	if err != nil {
		return fmt.Errorf("open stream to %s: %w", peerID, err)
	}
	defer stream.Close()
	deadline, _ := ctx.Deadline()
	_ = stream.SetDeadline(deadline)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = stream.Reset()
		case <-done:
		}
	}()

	if err := writeMessage(stream, max, Request{Method: method, Payload: rawPayload}); err != nil {
		_ = stream.Reset()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("encode request: %w", err)
	}

	var resp Response
	if err := readMessage(stream, max, &resp); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
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

// Each JSON frame ends in a newline. LimitReader bounds allocation even when a
// peer never finishes a frame; deadlines bound the time spent waiting for it.
func readMessage(r io.Reader, max int, out any) error {
	data, err := bufio.NewReader(io.LimitReader(r, int64(max)+1)).ReadBytes('\n')
	if len(data) > max {
		return ErrMessageTooLarge
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func writeMessage(w io.Writer, max int, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data)+1 > max {
		return ErrMessageTooLarge
	}
	_, err = io.Copy(w, bytes.NewReader(append(data, '\n')))
	return err
}

func OK(payload any) (Response, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("encode response payload: %w", err)
	}
	return Response{OK: true, Payload: raw}, nil
}
