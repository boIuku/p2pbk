// Package recovery exports the durable metadata required to recover a remote
// repository on a replacement computer. The bundle is encrypted and authenticated
// with age; it contains ownership keys and must be kept outside the protected host.
package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"filippo.io/age"
	"github.com/gofrs/flock"
	"p2pbk/internal/config"
	"p2pbk/internal/durable"
	"p2pbk/internal/peer/localdb"
)

const MaxBundleBytes = 64 << 20

var files = []string{"network.key", "storage.mode", "manifests.json", "locators.json"}

type bundle struct {
	Version  int
	Config   config.Config
	Files    map[string][]byte
	Metadata CheckpointMetadata
}

type CheckpointMetadata struct {
	Generation       int64     `json:"generation"`
	SnapshotIDs      []string  `json:"snapshot_ids"`
	ExportedAt       time.Time `json:"exported_at"`
	ValidationResult string    `json:"validation_result"`
}

// Export checkpoints a quiescent metadata transaction. Refresh the external
// bundle after backups: it guarantees recovery only through this checkpoint.
func Export(ctx context.Context, cfg config.Config, password, path string) error {
	return ExportWithMetadata(ctx, cfg, password, path, CheckpointMetadata{})
}

func ExportWithMetadata(ctx context.Context, cfg config.Config, password, path string, metadata CheckpointMetadata) error {
	if password == "" {
		return errors.New("recovery password required")
	}
	paths, err := cfg.Paths()
	if err != nil {
		return err
	}
	lock := flock.New(filepath.Join(paths.DataDir, "manifests.json.lock"))
	ok, err := lock.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("recovery metadata locked")
	}
	unlocked := false
	defer func() {
		if !unlocked {
			_ = lock.Unlock()
		}
	}()
	if metadata.Generation == 0 {
		metadata.Generation = time.Now().UTC().UnixNano()
	}
	if metadata.ExportedAt.IsZero() {
		metadata.ExportedAt = time.Now().UTC()
	}
	if metadata.ValidationResult == "" {
		metadata.ValidationResult = "pending"
	}
	b := bundle{Version: 1, Config: cfg, Files: map[string][]byte{}, Metadata: metadata}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(paths.DataDir, name))
		if err != nil {
			return fmt.Errorf("recovery metadata missing %s: %w", name, err)
		}
		b.Files[name] = data
	}
	if string(b.Files["storage.mode"]) != "remote" {
		return errors.New("recovery bundle requires a remote repository; local shards are not included")
	}
	b.Files["identity.age"], err = os.ReadFile(paths.Identity)
	if err != nil {
		return err
	}
	db, err := localdb.Open(paths.LocalDB)
	if err != nil {
		return err
	}
	peers, err := db.ListPeers()
	_ = db.Close()
	if err != nil {
		return err
	}
	for _, p := range peers {
		b.Config.Network.BootstrapPeers = append(b.Config.Network.BootstrapPeers, p.Addrs...)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := lock.Unlock(); err != nil {
		return err
	}
	unlocked = true
	b.Config.Trust.IdentityPassphrase = ""
	b.Metadata.ValidationResult = "validated-metadata"
	plain, err := json.Marshal(b)
	if err != nil {
		return err
	}
	if len(plain) > MaxBundleBytes {
		return errors.New("recovery metadata exceeds bundle limit")
	}
	recipient, err := age.NewScryptRecipient(password)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	w, err := age.Encrypt(&out, recipient)
	if err != nil {
		return err
	}
	if _, err = w.Write(plain); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return durable.WriteFile(path, out.Bytes(), 0600)
}

// Verify authenticates and parses a checkpoint without opening or mutating a
// live repository. It deliberately reports only bundle scope and metadata.
func Verify(ctx context.Context, path, password string) (CheckpointMetadata, error) {
	if password == "" {
		return CheckpointMetadata{}, errors.New("recovery password required")
	}
	if err := ctx.Err(); err != nil {
		return CheckpointMetadata{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return CheckpointMetadata{}, err
	}
	defer f.Close()
	id, err := age.NewScryptIdentity(password)
	if err != nil {
		return CheckpointMetadata{}, err
	}
	r, err := age.Decrypt(f, id)
	if err != nil {
		return CheckpointMetadata{}, fmt.Errorf("unlock recovery bundle: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxBundleBytes+1))
	if err != nil {
		return CheckpointMetadata{}, err
	}
	if len(data) > MaxBundleBytes {
		return CheckpointMetadata{}, errors.New("recovery bundle too large")
	}
	var b bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return CheckpointMetadata{}, fmt.Errorf("parse recovery bundle: %w", err)
	}
	if err := validateBundle(b); err != nil {
		return CheckpointMetadata{}, err
	}
	return b.Metadata, nil
}

// Import creates a NEW data directory. It never merges into or replaces a live
// repository. Reconnect with repo init after import; encrypted blobs already exist.
func Import(ctx context.Context, path, password, target string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("recovery target must not exist")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	id, err := age.NewScryptIdentity(password)
	if err != nil {
		return "", err
	}
	r, err := age.Decrypt(f, id)
	if err != nil {
		return "", fmt.Errorf("unlock recovery bundle: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxBundleBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxBundleBytes {
		return "", errors.New("recovery bundle too large")
	}
	var b bundle
	if err = json.Unmarshal(data, &b); err != nil {
		return "", err
	}
	if err := validateBundle(b); err != nil {
		return "", err
	}
	required := append(append([]string{}, files...), "identity.age")
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(filepath.Dir(target), ".recovery-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	for _, name := range required {
		if err = durable.WriteFile(filepath.Join(staging, name), b.Files[name], 0600); err != nil {
			return "", err
		}
	}
	b.Config.Node.DataDir = target
	b.Config.Node.Identity = "identity.age"
	b.Config.Storage.StoreDir = "store"
	b.Config.Storage.CacheDir = "cache"
	b.Config.Storage.Mode = "remote"
	b.Config.Backup.Roots = nil
	b.Config.Backup.Schedule = "manual"
	cfgPath := filepath.Join(target, "config.json")
	raw, err := json.MarshalIndent(b.Config, "", "  ")
	if err != nil {
		return "", err
	}
	if err = b.Config.Validate(); err != nil {
		return "", err
	}
	if err = durable.WriteFile(filepath.Join(staging, "config.json"), raw, 0600); err != nil {
		return "", err
	}
	if err = os.Rename(staging, target); err != nil {
		return "", err
	}
	return cfgPath, nil
}

func validateBundle(b bundle) error {
	if b.Version != 1 {
		return errors.New("unsupported recovery bundle version")
	}
	required := append(append([]string{}, files...), "identity.age")
	if len(b.Files) != len(required) {
		return errors.New("invalid recovery bundle file set")
	}
	for _, name := range required {
		if len(b.Files[name]) == 0 {
			return fmt.Errorf("recovery metadata missing %s", name)
		}
	}
	if string(b.Files["storage.mode"]) != "remote" {
		return errors.New("bundle is not a remote repository")
	}
	return nil
}
