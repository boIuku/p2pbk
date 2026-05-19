package challenges

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"p2pbk/internal/peer/blobserver"
)

type Challenge struct {
	Ref      blobserver.ShardRef `json:"ref"`
	Nonce    string              `json:"nonce"`
	Offset   int64               `json:"offset"`
	Length   int64               `json:"length"`
	IssuedAt time.Time           `json:"issued_at"`
}

type Response struct {
	Ref   blobserver.ShardRef `json:"ref"`
	Nonce string              `json:"nonce"`
	Proof string              `json:"proof"`
}

func New(ref blobserver.ShardRef, shardSize, maxLength int64, now time.Time) (Challenge, error) {
	if err := ref.Validate(); err != nil {
		return Challenge{}, err
	}
	if shardSize < 0 {
		return Challenge{}, errors.New("shardSize must be non-negative")
	}
	if maxLength <= 0 {
		return Challenge{}, errors.New("maxLength must be positive")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return Challenge{}, fmt.Errorf("read nonce: %w", err)
	}

	length := maxLength
	if shardSize < length {
		length = shardSize
	}
	offset := int64(0)
	if shardSize > length {
		span := shardSize - length + 1
		offset = int64(nonce[0]) % span
	}

	return Challenge{
		Ref:      ref,
		Nonce:    hex.EncodeToString(nonce),
		Offset:   offset,
		Length:   length,
		IssuedAt: now.UTC(),
	}, nil
}

func Answer(ch Challenge, shard []byte) (Response, error) {
	if err := ch.Validate(); err != nil {
		return Response{}, err
	}
	if ch.Offset+ch.Length > int64(len(shard)) {
		return Response{}, errors.New("challenge range exceeds shard size")
	}

	proof, err := Proof(ch, bytes.NewReader(shard))
	if err != nil {
		return Response{}, err
	}
	return Response{Ref: ch.Ref, Nonce: ch.Nonce, Proof: proof}, nil
}

func Verify(ch Challenge, resp Response, shard []byte) error {
	if resp.Ref != ch.Ref {
		return errors.New("challenge response ref mismatch")
	}
	if resp.Nonce != ch.Nonce {
		return errors.New("challenge response nonce mismatch")
	}

	expected, err := Answer(ch, shard)
	if err != nil {
		return err
	}
	if resp.Proof != expected.Proof {
		return errors.New("challenge proof mismatch")
	}
	return nil
}

func Proof(ch Challenge, reader io.ReaderAt) (string, error) {
	if err := ch.Validate(); err != nil {
		return "", err
	}

	nonce, err := hex.DecodeString(ch.Nonce)
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}

	buf := make([]byte, ch.Length)
	n, err := reader.ReadAt(buf, ch.Offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if int64(n) != ch.Length {
		return "", errors.New("short challenge read")
	}

	h := sha256.New()
	_, _ = h.Write(nonce)
	_, _ = h.Write(buf)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (ch Challenge) Validate() error {
	if err := ch.Ref.Validate(); err != nil {
		return err
	}
	if ch.Nonce == "" {
		return errors.New("nonce is required")
	}
	if ch.Offset < 0 {
		return errors.New("offset must be non-negative")
	}
	if ch.Length < 0 {
		return errors.New("length must be non-negative")
	}
	if ch.Length == 0 && ch.Offset != 0 {
		return errors.New("offset must be zero for empty challenge range")
	}
	return nil
}
