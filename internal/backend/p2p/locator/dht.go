package locator

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"p2pbk/internal/trust/identity"
)

type ValueRouter interface {
	PutValue(context.Context, string, []byte) error
	GetValue(context.Context, string) ([]byte, error)
}

type DHT struct {
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
	return &DHT{router: router, signer: signer, verifyPK: verifyPK}, nil
}

func (d *DHT) Put(ctx context.Context, blobID string, locations []Location) error {
	record, err := signRecord(d.signer, blobID, locations)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal dht locator record: %w", err)
	}
	return d.router.PutValue(ctx, dhtKey(blobID), payload)
}

func (d *DHT) Get(ctx context.Context, blobID string) ([]Location, error) {
	payload, err := d.router.GetValue(ctx, dhtKey(blobID))
	if err != nil {
		return nil, err
	}
	var record SignedRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, fmt.Errorf("unmarshal dht locator record: %w", err)
	}
	if err := VerifyRecord(record, d.verifyPK); err != nil {
		return nil, err
	}
	return record.locations()
}

func (d *DHT) Delete(ctx context.Context, blobID string) error {
	return d.router.PutValue(ctx, dhtKey(blobID), nil)
}

func dhtKey(blobID string) string {
	return "/p2pbk/locator/" + url.PathEscape(blobID)
}
