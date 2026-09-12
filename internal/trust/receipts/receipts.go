package receipts

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/libp2p/go-libp2p/core/crypto"
	"time"

	"p2pbk/internal/trust/identity"
)

type ShardRef struct {
	Owner  string `json:"owner"`
	BlobID string `json:"blob_id"`
	Index  int    `json:"index"`
}

func IssueWithKey(key crypto.PrivKey, ref ShardRef, hash, storedBy string, at time.Time) (Receipt, error) {
	r := Receipt{Ref: ref, Hash: hash, StoredBy: storedBy, IssuedAt: at.UTC()}
	message, err := r.signingPayload()
	if err != nil {
		return Receipt{}, err
	}
	sig, err := key.Sign(message)
	if err != nil {
		return Receipt{}, err
	}
	r.Signature = base64.StdEncoding.EncodeToString(sig)
	return r, nil
}

func VerifyWithKey(key crypto.PubKey, r Receipt) error {
	if key == nil {
		return errors.New("receipt peer key unavailable")
	}
	sig, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil {
		return err
	}
	msg, err := r.signingPayload()
	if err != nil {
		return err
	}
	ok, err := key.Verify(msg, sig)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("receipt signature verification failed")
	}
	return nil
}

type Receipt struct {
	Ref       ShardRef  `json:"ref"`
	Hash      string    `json:"hash"`
	StoredBy  string    `json:"stored_by"`
	IssuedAt  time.Time `json:"issued_at"`
	Signature string    `json:"signature"`
}

type payload struct {
	Ref      ShardRef  `json:"ref"`
	Hash     string    `json:"hash"`
	StoredBy string    `json:"stored_by"`
	IssuedAt time.Time `json:"issued_at"`
}

func Issue(id *identity.Identity, ref ShardRef, hash, storedBy string, issuedAt time.Time) (Receipt, error) {
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

func (r ShardRef) Validate() error {
	if r.Owner == "" {
		return errors.New("owner is required")
	}
	if r.BlobID == "" {
		return errors.New("blob_id is required")
	}
	if r.Index < 0 {
		return errors.New("index must be non-negative")
	}
	return nil
}
