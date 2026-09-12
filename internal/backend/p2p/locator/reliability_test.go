package locator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/routing"
	"p2pbk/internal/trust/identity"
)

type failingRouter struct {
	values map[string][]byte
	err    error
}

func (r *failingRouter) GetValue(_ context.Context, key string) ([]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	data, ok := r.values[key]
	if !ok {
		return nil, routing.ErrNotFound
	}
	return data, nil
}
func (r *failingRouter) PutValue(_ context.Context, key string, data []byte) error {
	if r.err != nil {
		return r.err
	}
	r.values[key] = append([]byte(nil), data...)
	return nil
}

func TestLocatorIsolationAcrossRepositories(t *testing.T) {
	ctx := context.Background()
	router := &failingRouter{values: map[string][]byte{}}
	a, err := identity.Create(filepath.Join(t.TempDir(), "a.age"), "pass")
	if err != nil {
		t.Fatal(err)
	}
	b, err := identity.Create(filepath.Join(t.TempDir(), "b.age"), "pass")
	if err != nil {
		t.Fatal(err)
	}
	la, _ := NewDHT(router, a, nil)
	lb, _ := NewDHT(router, b, nil)
	if err = la.Put(ctx, "kopia.repository", nil); err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), router.values[la.key("kopia.repository")]...)
	if err = lb.Put(ctx, "kopia.repository", nil); err != nil {
		t.Fatal(err)
	}
	if len(router.values) != 2 {
		t.Fatal("two owners collided on a common Kopia blob name")
	}
	if string(router.values[la.key("kopia.repository")]) != string(original) {
		t.Fatal("owner B overwrote A's locator")
	}
	if _, err = la.Get(ctx, "kopia.repository"); err != nil {
		t.Fatal(err)
	}
}

// Readers and DHT validators both bind records to the requested key and reject
// tampering. A previously observed newer revision must not be rolled back.
func TestLocatorRejectsWrongKeyAndReplay(t *testing.T) {
	ctx := context.Background()
	id, err := identity.Create(filepath.Join(t.TempDir(), "id.age"), "pass")
	if err != nil {
		t.Fatal(err)
	}
	router := &failingRouter{values: map[string][]byte{}}
	d, _ := NewDHT(router, id, nil)
	if err = d.Put(ctx, "blob", nil); err != nil {
		t.Fatal(err)
	}
	key := d.key("blob")
	old := append([]byte(nil), router.values[key]...)
	if err = d.Put(ctx, "blob", nil); err != nil {
		t.Fatal(err)
	}
	current := append([]byte(nil), router.values[key]...)
	cases := map[string][]byte{"replay": old, "malformed": []byte("not JSON")}
	var rec SignedRecord
	_ = json.Unmarshal(current, &rec)
	rec.BlobID = "different"
	raw, _ := json.Marshal(rec)
	cases["wrong_blob"] = raw
	_ = json.Unmarshal(current, &rec)
	rec.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
	raw, _ = json.Marshal(rec)
	cases["signature"] = raw
	_ = json.Unmarshal(current, &rec)
	rec.IssuedAt = time.Now().Add(time.Hour)
	msg, _ := rec.signingPayload()
	rec.Signature = base64.StdEncoding.EncodeToString(id.Sign(msg))
	raw, _ = json.Marshal(rec)
	cases["signed_future_time"] = raw
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			router.values[key] = bad
			if _, err := d.Get(ctx, "blob"); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
	router.values[key] = current
	if _, err = d.Get(ctx, "blob"); err != nil {
		t.Fatal(err)
	}
	if err = ValidateValue(d.key("different"), current); err == nil {
		t.Fatal("valid signature accepted at wrong DHT key")
	}
	if err = d.Delete(ctx, "blob"); err != nil {
		t.Fatal(err)
	}
	if err = ValidateValue(key, router.values[key]); err != nil {
		t.Fatalf("deletion must publish an authenticated tombstone: %v", err)
	}
}

func TestDurableLocatorSurvivesDHTLoss(t *testing.T) {
	id, err := identity.Create(filepath.Join(t.TempDir(), "id.age"), "pass")
	if err != nil {
		t.Fatal(err)
	}
	router := &failingRouter{values: map[string][]byte{}}
	path := filepath.Join(t.TempDir(), "locators.json")
	d, err := NewPersistentDHT(router, id, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Put(context.Background(), "blob", nil); err != nil {
		t.Fatal(err)
	}
	router.values = nil
	router.err = errors.New("DHT unavailable")
	reopened, err := NewPersistentDHT(router, id, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Get(context.Background(), "blob"); err != nil {
		t.Fatalf("durable locations lost with DHT: %v", err)
	}
	if _, err = reopened.Get(context.Background(), "unknown"); !errors.Is(err, routing.ErrNotFound) {
		t.Fatalf("unknown blob: %v", err)
	}
}
