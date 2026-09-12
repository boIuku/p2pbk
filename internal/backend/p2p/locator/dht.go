package locator

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/libp2p/go-libp2p/core/routing"
	"net/url"
	"os"
	"p2pbk/internal/durable"
	"strings"
	"sync"
	"time"

	"p2pbk/internal/trust/identity"
)

type ValueRouter interface {
	PutValue(context.Context, string, []byte) error
	GetValue(context.Context, string) ([]byte, error)
}

type DHT struct {
	mu       sync.Mutex
	path     string
	records  map[string]SignedRecord
	router   ValueRouter
	signer   *identity.Identity
	verifyPK ed25519.PublicKey
}

func NewDHT(router ValueRouter, signer *identity.Identity, verifyPK ed25519.PublicKey) (*DHT, error) {
	if router == nil {
		return nil, errors.New("value router is required")
	}
	if signer == nil {
		return nil, errors.New("locator signer is required")
	}
	if len(verifyPK) == 0 {
		verifyPK = signer.PublicKey()
	}
	return &DHT{router: router, signer: signer, verifyPK: verifyPK, records: map[string]SignedRecord{}}, nil
}

// NewPersistentDHT keeps authenticated recovery metadata on disk. The DHT is a
// publication/discovery layer, never the sole durable copy of accepted locations.
func NewPersistentDHT(router ValueRouter, signer *identity.Identity, path string) (*DHT, error) {
	d, err := NewDHT(router, signer, nil)
	if err != nil {
		return nil, err
	}
	d.path = path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &d.records); err != nil {
		return nil, err
	}
	if d.records == nil {
		d.records = map[string]SignedRecord{}
	}
	for key, r := range d.records {
		data, _ := json.Marshal(r)
		if err = ValidateValue(d.key(key), data); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (d *DHT) Put(ctx context.Context, blobID string, locations []Location) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.reload(); err != nil {
		return err
	}
	record, err := signRecord(d.signer, blobID, locations)
	if err != nil {
		return err
	}
	if old, ok := d.records[blobID]; ok && record.Revision <= old.Revision {
		record.Revision = old.Revision + 1
		message, err := record.signingPayload()
		if err != nil {
			return err
		}
		record.Signature = base64.StdEncoding.EncodeToString(d.signer.Sign(message))
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal dht locator record: %w", err)
	}
	if d.path != "" {
		old, had := d.records[blobID]
		d.records[blobID] = record
		data, _ := json.Marshal(d.records)
		if err := durable.WriteFile(d.path, data, 0600); err != nil {
			if had {
				d.records[blobID] = old
			} else {
				delete(d.records, blobID)
			}
			return err
		}
		publish, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_ = d.router.PutValue(publish, d.key(blobID), payload)
		return nil
	}
	if err := d.router.PutValue(ctx, d.key(blobID), payload); err != nil {
		return err
	}
	d.records[blobID] = record
	return nil
}

func (d *DHT) Get(ctx context.Context, blobID string) ([]Location, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.reload(); err != nil {
		return nil, err
	}
	if d.path != "" {
		if record, ok := d.records[blobID]; ok {
			return record.locations()
		}
		return nil, routing.ErrNotFound
	}
	payload, err := d.router.GetValue(ctx, d.key(blobID))
	if err != nil {
		return nil, err
	}
	var record SignedRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, fmt.Errorf("unmarshal dht locator record: %w", err)
	}
	if err := ValidateValue(d.key(blobID), payload); err != nil {
		return nil, err
	}
	if err := VerifyRecord(record, d.verifyPK); err != nil {
		return nil, err
	}
	if old, ok := d.records[blobID]; ok && record.Revision < old.Revision {
		return nil, errors.New("locator replay rejected")
	}
	d.records[blobID] = record
	return record.locations()
}

func (d *DHT) reload() error {
	if d.path == "" {
		return nil
	}
	data, err := os.ReadFile(d.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	records := map[string]SignedRecord{}
	if err = json.Unmarshal(data, &records); err != nil {
		return err
	}
	for key, r := range records {
		raw, _ := json.Marshal(r)
		if err = ValidateValue(d.key(key), raw); err != nil {
			return err
		}
		if old, ok := d.records[key]; ok && r.Revision < old.Revision {
			return errors.New("durable locator replay rejected")
		}
	}
	d.records = records
	return nil
}

func (d *DHT) Delete(ctx context.Context, blobID string) error {
	return d.Put(ctx, blobID, nil)
}

func dhtKey(blobID string) string {
	return "/p2pbk/locator/" + url.PathEscape(blobID)
}

func (d *DHT) key(blobID string) string {
	sum := sha256.Sum256(d.verifyPK)
	return dhtKey(hex.EncodeToString(sum[:]) + "/" + blobID)
}

// ValidateValue binds the signed payload to its namespace, owner key and blob ID.
// Network validators call this before admitting records, not only after selection.
func ValidateValue(key string, value []byte) error {
	if len(value) == 0 || len(value) > 1<<20 {
		return errors.New("invalid locator size")
	}
	var r SignedRecord
	if err := json.Unmarshal(value, &r); err != nil {
		return err
	}
	pk, err := base64.StdEncoding.DecodeString(r.PublicKey)
	if err != nil || len(pk) != ed25519.PublicKeySize {
		return errors.New("invalid locator public key")
	}
	sum := sha256.Sum256(pk)
	expected := dhtKey(hex.EncodeToString(sum[:]) + "/" + r.BlobID)
	if key != expected || r.SignedBy != hex.EncodeToString(sum[:]) || strings.TrimSpace(r.BlobID) == "" {
		return errors.New("locator key does not match signed owner/blob")
	}
	if r.Revision == 0 || r.IssuedAt.IsZero() || r.IssuedAt.After(time.Now().Add(5*time.Minute)) {
		return errors.New("invalid locator revision/time")
	}
	for _, loc := range r.Locations {
		if loc.Ref.BlobID != r.BlobID {
			return errors.New("locator ref does not match blob")
		}
		if err := loc.Ref.Validate(); err != nil {
			return err
		}
	}
	return VerifyRecord(r, ed25519.PublicKey(pk))
}
