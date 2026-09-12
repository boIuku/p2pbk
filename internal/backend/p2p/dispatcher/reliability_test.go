package dispatcher

import (
	"context"
	"strings"
	"testing"

	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/quota"
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
	for _, client := range []wire.Client{wire.NewClient(a), wire.NewClient(b)} {
		_ = client
	}
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
	router.Handle(blobserver.MethodPutShard, func(context.Context, wire.Request) (wire.Response, error) {
		return wire.OK(blobserver.PutShardResponse{})
	})
	router.Register(h)
	_, err = New(client).PutShard(ctx, h.ID(), blobserver.PutShardRequest{Ref: blobserver.ShardRef{Owner: client.ID().String(), BlobID: "blob", Index: 0}, Data: []byte("hello")})
	if err == nil {
		t.Fatal("peer acknowledged upload without a valid assignment receipt")
	}
}
