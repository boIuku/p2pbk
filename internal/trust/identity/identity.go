package identity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"filippo.io/age"
)

var ErrExists = errors.New("identity already exists")

type Identity struct {
	private ed25519.PrivateKey
}

type encryptedFile struct {
	Version    int    `json:"version"`
	Algorithm  string `json:"algorithm"`
	PrivateKey string `json:"private_key"`
}

func Create(path, passphrase string) (*Identity, error) {
	if _, err := os.Stat(path); err == nil {
		return nil, ErrExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate identity: %w", err)
	}

	id := &Identity{private: private}
	if err := id.Save(path, passphrase); err != nil {
		return nil, err
	}
	return id, nil
}

func Load(path, passphrase string) (*Identity, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("identity passphrase: %w", err)
	}

	reader, err := age.Decrypt(file, identity)
	if err != nil {
		return nil, fmt.Errorf("decrypt identity: %w", err)
	}

	var payload encryptedFile
	if err := json.NewDecoder(reader).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode identity: %w", err)
	}
	if payload.Version != 1 {
		return nil, fmt.Errorf("unsupported identity version %d", payload.Version)
	}
	if payload.Algorithm != "ed25519" {
		return nil, fmt.Errorf("unsupported identity algorithm %q", payload.Algorithm)
	}

	key, err := base64.StdEncoding.DecodeString(payload.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key has %d bytes, want %d", len(key), ed25519.PrivateKeySize)
	}

	return &Identity{private: ed25519.PrivateKey(key)}, nil
}

func (i *Identity) Save(path, passphrase string) error {
	if len(i.private) != ed25519.PrivateKeySize {
		return fmt.Errorf("private key has %d bytes, want %d", len(i.private), ed25519.PrivateKeySize)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create identity directory: %w", err)
	}

	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return fmt.Errorf("identity passphrase: %w", err)
	}

	payload := encryptedFile{
		Version:    1,
		Algorithm:  "ed25519",
		PrivateKey: base64.StdEncoding.EncodeToString(i.private),
	}

	var plain bytes.Buffer
	if err := json.NewEncoder(&plain).Encode(payload); err != nil {
		return fmt.Errorf("encode identity: %w", err)
	}

	var encrypted bytes.Buffer
	writer, err := age.Encrypt(&encrypted, recipient)
	if err != nil {
		return fmt.Errorf("prepare identity encryption: %w", err)
	}
	if _, err := io.Copy(writer, &plain); err != nil {
		return fmt.Errorf("encrypt identity: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish identity encryption: %w", err)
	}

	return os.WriteFile(path, encrypted.Bytes(), 0o600)
}

func (i *Identity) PublicKey() ed25519.PublicKey {
	return i.private.Public().(ed25519.PublicKey)
}

func (i *Identity) Fingerprint() string {
	sum := sha256.Sum256(i.PublicKey())
	return hex.EncodeToString(sum[:])
}

func (i *Identity) Sign(message []byte) []byte {
	return ed25519.Sign(i.private, message)
}

func Verify(public ed25519.PublicKey, message, sig []byte) bool {
	return ed25519.Verify(public, message, sig)
}
