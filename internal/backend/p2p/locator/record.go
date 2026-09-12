package locator

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/identity"
)

type Location struct {
	Peer peer.ID
	Ref  blobserver.ShardRef
}

type Record struct {
	BlobID    string
	Locations []Location
}

type SignedRecord struct {
	PublicKey string           `json:"public_key"`
	Revision  uint64           `json:"revision"`
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

func signRecord(signer *identity.Identity, blobID string, locations []Location) (SignedRecord, error) {
	if signer == nil {
		return SignedRecord{}, errors.New("locator signer is required")
	}
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
		PublicKey: base64.StdEncoding.EncodeToString(signer.PublicKey()),
		Revision:  uint64(time.Now().UnixNano()),
		BlobID:    blobID,
		Locations: signedLocations,
		IssuedAt:  time.Now().UTC(),
		SignedBy:  signer.Fingerprint(),
	}
	message, err := record.signingPayload()
	if err != nil {
		return SignedRecord{}, err
	}
	record.Signature = base64.StdEncoding.EncodeToString(signer.Sign(message))
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
		PublicKey string           `json:"public_key"`
		Revision  uint64           `json:"revision"`
		BlobID    string           `json:"blob_id"`
		Locations []SignedLocation `json:"locations"`
		IssuedAt  time.Time        `json:"issued_at"`
		SignedBy  string           `json:"signed_by"`
	}{
		PublicKey: r.PublicKey,
		Revision:  r.Revision,
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
