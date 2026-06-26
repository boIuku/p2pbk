package locator

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	_ "modernc.org/sqlite"

	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/identity"
)

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
	db       *sql.DB
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
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open locator db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
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
	_, err = p.db.Exec(
		`INSERT INTO locator_records(blob_id, issued_at, data) VALUES(?, ?, ?)
		 ON CONFLICT(blob_id) DO UPDATE SET issued_at = excluded.issued_at, data = excluded.data`,
		blobID,
		record.IssuedAt.UTC().Format(time.RFC3339Nano),
		payload,
	)
	return err
}

func (p *Persistent) Get(ctx context.Context, blobID string) ([]Location, error) {
	var payload []byte
	err := p.db.QueryRow(`SELECT data FROM locator_records WHERE blob_id = ?`, blobID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
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
	_, err := p.db.Exec(`DELETE FROM locator_records WHERE blob_id = ?`, blobID)
	return err
}

func (p *Persistent) init() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, stmt := range pragmas {
		if _, err := p.db.Exec(stmt); err != nil {
			return fmt.Errorf("apply sqlite pragma %q: %w", stmt, err)
		}
	}
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS locator_records (
		blob_id TEXT PRIMARY KEY,
		issued_at TEXT NOT NULL,
		data BLOB NOT NULL
	)`)
	return err
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
