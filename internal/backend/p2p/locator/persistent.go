package locator

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	bolt "go.etcd.io/bbolt"

	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/identity"
)

var recordsBucket = []byte("locator_records")

type SignedRecord struct {
	BlobID    string           `json:"blob_id"`
	Locations []SignedLocation `json:"locations"`
	IssuedAt  time.Time        `json:"issued_at"`
	SignedBy  string           `json:"signed_by"`
	Signature string           `json:"signature"`
}

type SignedLocation struct {
	Peer string              `json:"peer"`
	Ref  blobserver.ShardRef `json:"ref"`
}

type Persistent struct {
	db       *bolt.DB
	signer   *identity.Identity
	verifyPK ed25519.PublicKey
}

func OpenPersistent(path string, signer *identity.Identity, verifyPK ed25519.PublicKey) (*Persistent, error) {
	if path == "" {
		return nil, errors.New("locator db path is required")
	}
	if signer == nil {
		return nil, errors.New("locator signer is required")
	}
	if len(verifyPK) == 0 {
		verifyPK = signer.PublicKey()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create locator directory: %w", err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open locator db: %w", err)
	}
	p := &Persistent{db: db, signer: signer, verifyPK: verifyPK}
	if err := p.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return p, nil
}

func (p *Persistent) Close() error {
	return p.db.Close()
}

func (p *Persistent) Put(ctx context.Context, blobID string, locations []Location) error {
	record, err := p.sign(blobID, locations)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal locator record: %w", err)
	}
	return p.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(recordsBucket).Put([]byte(blobID), payload)
	})
}

func (p *Persistent) Get(ctx context.Context, blobID string) ([]Location, error) {
	var payload []byte
	if err := p.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(recordsBucket).Get([]byte(blobID))
		if value != nil {
			payload = append([]byte(nil), value...)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, nil
	}

	var record SignedRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, fmt.Errorf("unmarshal locator record: %w", err)
	}
	if err := VerifyRecord(record, p.verifyPK); err != nil {
		return nil, err
	}
	return record.locations()
}

func (p *Persistent) Delete(ctx context.Context, blobID string) error {
	return p.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(recordsBucket).Delete([]byte(blobID))
	})
}

func (p *Persistent) init() error {
	return p.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(recordsBucket)
		return err
	})
}

func (p *Persistent) sign(blobID string, locations []Location) (SignedRecord, error) {
	if blobID == "" {
		return SignedRecord{}, errors.New("blobID is required")
	}
	signedLocations := make([]SignedLocation, 0, len(locations))
	copyLocations := append([]Location(nil), locations...)
	sortLocations(copyLocations)
	for _, loc := range copyLocations {
		signedLocations = append(signedLocations, SignedLocation{Peer: loc.Peer.String(), Ref: loc.Ref})
	}
	record := SignedRecord{
		BlobID:    blobID,
		Locations: signedLocations,
		IssuedAt:  time.Now().UTC(),
		SignedBy:  p.signer.Fingerprint(),
	}
	message, err := record.signingPayload()
	if err != nil {
		return SignedRecord{}, err
	}
	record.Signature = base64.StdEncoding.EncodeToString(p.signer.Sign(message))
	return record, nil
}

func VerifyRecord(record SignedRecord, public ed25519.PublicKey) error {
	if record.BlobID == "" {
		return errors.New("record blob_id is required")
	}
	if record.Signature == "" {
		return errors.New("record signature is required")
	}
	sig, err := base64.StdEncoding.DecodeString(record.Signature)
	if err != nil {
		return fmt.Errorf("decode locator signature: %w", err)
	}
	message, err := record.signingPayload()
	if err != nil {
		return err
	}
	if !identity.Verify(public, message, sig) {
		return errors.New("locator signature verification failed")
	}
	return nil
}

func (r SignedRecord) signingPayload() ([]byte, error) {
	payload := struct {
		BlobID    string           `json:"blob_id"`
		Locations []SignedLocation `json:"locations"`
		IssuedAt  time.Time        `json:"issued_at"`
		SignedBy  string           `json:"signed_by"`
	}{
		BlobID:    r.BlobID,
		Locations: append([]SignedLocation(nil), r.Locations...),
		IssuedAt:  r.IssuedAt.UTC(),
		SignedBy:  r.SignedBy,
	}
	sort.Slice(payload.Locations, func(i, j int) bool {
		if payload.Locations[i].Ref.Index == payload.Locations[j].Ref.Index {
			return payload.Locations[i].Peer < payload.Locations[j].Peer
		}
		return payload.Locations[i].Ref.Index < payload.Locations[j].Ref.Index
	})
	return json.Marshal(payload)
}

func (r SignedRecord) locations() ([]Location, error) {
	out := make([]Location, 0, len(r.Locations))
	for _, loc := range r.Locations {
		id, err := peer.Decode(loc.Peer)
		if err != nil {
			return nil, fmt.Errorf("decode peer %q: %w", loc.Peer, err)
		}
		out = append(out, Location{Peer: id, Ref: loc.Ref})
	}
	sortLocations(out)
	return out, nil
}
