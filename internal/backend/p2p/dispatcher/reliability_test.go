package dispatcher

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/quota"
	"p2pbk/internal/trust/receipts"
)

// P0-02: knowing a shard's complete address never grants ownership of it.
// Rejection must preserve both its bytes and the storage peer's accounting.
func TestShardAuthorizationAcrossOwners(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	h, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	q, _ := quota.New(100)
	s, err := blobserver.NewWithQuota(t.TempDir(), q)
	if err != nil {
		t.Fatal(err)
	}
	router := wire.NewRouter()
	s.Register(router)
	router.Register(h)
	if err = a.Connect(ctx, p2phost.AddrInfo(h)); err != nil {
		t.Fatal(err)
	}
	if err = b.Connect(ctx, p2phost.AddrInfo(h)); err != nil {
		t.Fatal(err)
	}
	ref := blobserver.ShardRef{Owner: a.ID().String(), BlobID: "same-display-name", Index: 0}
	if _, err = New(a).PutShard(ctx, h.ID(), blobserver.PutShardRequest{Ref: ref, Data: []byte("original")}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		method  string
		payload any
	}{
		{"get", blobserver.MethodGetShard, blobserver.GetShardRequest{Ref: ref}},
		{"overwrite", blobserver.MethodPutShard, blobserver.PutShardRequest{Ref: ref, Data: []byte("attacker")}},
		{"delete", blobserver.MethodDeleteShard, blobserver.DeleteShardRequest{Ref: ref}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := wire.NewClient(b).Call(ctx, h.ID(), tc.method, tc.payload, nil)
			if err == nil || !strings.Contains(err.Error(), "authenticated peer") {
				t.Fatalf("want ownership rejection, got %v", err)
			}
			got, err := New(a).GetShard(ctx, h.ID(), blobserver.GetShardRequest{Ref: ref})
			if err != nil || string(got.Data) != "original" || q.Total() != 8 {
				t.Fatalf("rejected request changed state: %q usage=%d error=%v", got.Data, q.Total(), err)
			}
		})
	}
	ref.Owner = b.ID().String()
	if _, err := New(b).PutShard(ctx, h.ID(), blobserver.PutShardRequest{Ref: ref, Data: []byte("bob")}); err != nil {
		t.Fatal(err)
	}
	if q.Total() != 11 {
		t.Fatalf("independent owner collided with Alice: %d", q.Total())
	}
}

// P0-02: an acknowledgement proves the contacted peer signed this exact
// assignment. A valid signature for different bytes or ownership is insufficient.
func TestUploadRejectsForgedOrMismatchedReceipt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	h, err := p2phost.New(ctx, p2phost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err = client.Connect(ctx, p2phost.AddrInfo(h)); err != nil {
		t.Fatal(err)
	}
	router := wire.NewRouter()
	router.Handle(blobserver.MethodPutShard, func(_ context.Context, request wire.Request) (wire.Response, error) {
		var req blobserver.PutShardRequest
		if err := json.Unmarshal(request.Payload, &req); err != nil {
			return wire.Response{}, err
		}
		if req.Ref.BlobID == "missing" {
			return wire.OK(blobserver.PutShardResponse{})
		}
		ref := receipts.ShardRef{Owner: req.Ref.Owner, BlobID: req.Ref.BlobID, Index: req.Ref.Index}
		hash, storedBy := req.Hash, h.ID().String()
		key := h.Peerstore().PrivKey(h.ID())
		switch req.Ref.BlobID {
		case "wrong-owner":
			ref.Owner = h.ID().String()
		case "wrong-blob":
			ref.BlobID = "another-blob"
		case "wrong-index":
			ref.Index++
		case "wrong-hash":
			hash = blobserver.Hash([]byte("different bytes"))
		case "wrong-storage-peer":
			storedBy = client.ID().String()
		case "forged-signature":
			key = client.Peerstore().PrivKey(client.ID())
		}
		receipt, err := receipts.IssueWithKey(key, ref, hash, storedBy, time.Now())
		if err != nil {
			return wire.Response{}, err
		}
		return wire.OK(blobserver.PutShardResponse{Receipt: receipt})
	})
	router.Register(h)
	for _, name := range []string{"valid", "missing", "wrong-owner", "wrong-blob", "wrong-index", "wrong-hash", "wrong-storage-peer", "forged-signature"} {
		t.Run(name, func(t *testing.T) {
			_, err := New(client).PutShard(ctx, h.ID(), blobserver.PutShardRequest{Ref: blobserver.ShardRef{Owner: client.ID().String(), BlobID: name, Index: 0}, Data: []byte("hello")})
			if name == "valid" {
				if err != nil {
					t.Fatalf("valid acknowledgement rejected: %v", err)
				}
				return
			}
			want := "receipt does not match shard assignment"
			if name == "forged-signature" {
				want = "receipt signature verification failed"
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("want %q rejection, got %v", want, err)
			}
		})
	}
}
