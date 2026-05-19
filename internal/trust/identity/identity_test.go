package identity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.age")
	passphrase := "correct horse battery staple"

	created, err := Create(path, passphrase)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	loaded, err := Load(path, passphrase)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if created.Fingerprint() != loaded.Fingerprint() {
		t.Fatalf("fingerprint mismatch: %q != %q", created.Fingerprint(), loaded.Fingerprint())
	}

	message := []byte("audit challenge")
	sig := loaded.Sign(message)
	if !Verify(loaded.PublicKey(), message, sig) {
		t.Fatal("signature did not verify")
	}
}

func TestCreateRejectsExistingIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.age")
	if _, err := Create(path, "passphrase"); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if _, err := Create(path, "passphrase"); !errors.Is(err, ErrExists) {
		t.Fatalf("Create error = %v, want ErrExists", err)
	}
}

func TestLoadRejectsWrongPassphrase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.age")
	if _, err := Create(path, "right-passphrase"); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if _, err := Load(path, "wrong-passphrase"); err == nil {
		t.Fatal("Load returned nil error for wrong passphrase")
	}
}

func TestIdentityFileIsNotPlaintextKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.age")
	created, err := Create(path, "passphrase")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) == string(created.private) {
		t.Fatal("identity file stored raw private key bytes")
	}
}
