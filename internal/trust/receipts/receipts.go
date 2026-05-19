package receipts

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/identity"
)

type Receipt struct {
	Ref       blobserver.ShardRef `json:"ref"`
	Hash      string              `json:"hash"`
	StoredBy  string              `json:"stored_by"`
	IssuedAt  time.Time           `json:"issued_at"`
	Signature string              `json:"signature"`
}

type payload struct {
	Ref      blobserver.ShardRef `json:"ref"`
	Hash     string              `json:"hash"`
	StoredBy string              `json:"stored_by"`
	IssuedAt time.Time           `json:"issued_at"`
}

func Issue(id *identity.Identity, ref blobserver.ShardRef, hash, storedBy string, issuedAt time.Time) (Receipt, error) {
	if id == nil {
		return Receipt{}, errors.New("identity is required")
	}
	if err := ref.Validate(); err != nil {
		return Receipt{}, err
	}
	if hash == "" {
		return Receipt{}, errors.New("hash is required")
	}
	if storedBy == "" {
		storedBy = id.Fingerprint()
	}
	if issuedAt.IsZero() {
		issuedAt = time.Now().UTC()
	}
	issuedAt = issuedAt.UTC()

	r := Receipt{Ref: ref, Hash: hash, StoredBy: storedBy, IssuedAt: issuedAt}
	message, err := r.signingPayload()
	if err != nil {
		return Receipt{}, err
	}
	r.Signature = base64.StdEncoding.EncodeToString(id.Sign(message))
	return r, nil
}

func Verify(public ed25519.PublicKey, r Receipt) error {
	if err := r.Ref.Validate(); err != nil {
		return err
	}
	if r.Hash == "" {
		return errors.New("hash is required")
	}
	if r.StoredBy == "" {
		return errors.New("stored_by is required")
	}
	if r.Signature == "" {
		return errors.New("signature is required")
	}

	sig, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	message, err := r.signingPayload()
	if err != nil {
		return err
	}
	if !identity.Verify(public, message, sig) {
		return errors.New("receipt signature verification failed")
	}
	return nil
}

func (r Receipt) signingPayload() ([]byte, error) {
	payload := payload{
		Ref:      r.Ref,
		Hash:     r.Hash,
		StoredBy: r.StoredBy,
		IssuedAt: r.IssuedAt.UTC(),
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal receipt payload: %w", err)
	}
	return out, nil
}
