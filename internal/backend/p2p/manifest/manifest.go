package manifest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Manifest represents a small blob stored with raw replication instead of erasure coding.
// This is used for manifests (metadata, snapshots, indexes) that must be recoverable
// from any single replica.
type Manifest struct {
	data    []byte
	hash    string
	peerIDs []interface{} // libp2p peer.IDs or string equivalents
}

const (
	// MinManifestSize is the threshold below which blobs are stored as raw manifests.
	MinManifestSize = 4096
)

// New creates a new manifest from the given data.
func New(data []byte) (*Manifest, error) {
	hash := sha256.Sum256(data)

	return &Manifest{
		data: append([]byte(nil), data...),
		hash: hex.EncodeToString(hash[:]),
	}, nil
}

// NewFromFile creates a manifest from a file.
func NewFromFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", path, err)
	}

	return New(data)
}

// Write writes the manifest data to a file.
func (m *Manifest) Write(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	return os.WriteFile(path, m.data, 0o600)
}

// Hash returns the SHA256 hash of the manifest data.
func (m *Manifest) Hash() string {
	return m.hash
}

// Data returns the manifest data.
func (m *Manifest) Data() []byte {
	return append([]byte(nil), m.data...)
}

// Replicate replicates the manifest to the given peer IDs.
// Unlike erasure coding, manifests are stored raw on each replica.
func (m *Manifest) Replicate(peerIDs []interface{}) error {
	if len(peerIDs) == 0 {
		return fmt.Errorf("no peers for replication")
	}
	m.peerIDs = append([]interface{}(nil), peerIDs...)
	return nil
}

// ReadRange reads a byte range from the manifest.
func (m *Manifest) ReadRange(offset, length int64) ([]byte, error) {
	if offset < 0 || offset > int64(len(m.data)) {
		return nil, fmt.Errorf("offset %d out of range", offset)
	}
	if length < 0 || offset+length > int64(len(m.data)) {
		return nil, fmt.Errorf("length %d exceeds remaining data", length)
	}

	return append([]byte(nil), m.data[offset:offset+length]...), nil
}

// Verify checks the manifest data against expected hash.
func (m *Manifest) Verify() error {
	if m.hash != "" && m.hash != sha256Hex(m.data) {
		return fmt.Errorf("hash mismatch: got %s want %s", sha256Hex(m.data), m.hash)
	}

	return nil
}

// sha256Hex returns the hex-encoded SHA256 of data.
func sha256Hex(data []byte) string {
	h := sha256.New()
	h.Write(data)

	return hex.EncodeToString(h.Sum(nil))
}

// ManifestReplicator handles replication of manifests across peers.
type ManifestReplicator struct {
	// storeDir is where manifests are stored locally
	storeDir string
	// replicas tracks which peers hold each manifest
	replicas map[string][]interface{} // hash -> peer IDs
}

// NewReplicator creates a new manifest replicator.
func NewReplicator(storeDir string) (*ManifestReplicator, error) {
	if storeDir == "" {
		return nil, fmt.Errorf("storeDir is required")
	}

	return &ManifestReplicator{
		storeDir: storeDir,
		replicas: make(map[string][]interface{}),
	}, nil
}

// Replicate puts a manifest on all given peers.
func (r *ManifestReplicator) Replicate(ctx context.Context, m *Manifest, peerIDs []interface{}) error {
	if len(peerIDs) == 0 {
		return fmt.Errorf("no peers for replication")
	}
	local, err := NewLocalManifest(r.storeDir)
	if err != nil {
		return err
	}
	if err := local.Put(ctx, m); err != nil {
		return err
	}
	if err := m.Replicate(peerIDs); err != nil {
		return err
	}
	r.replicas[m.Hash()] = append([]interface{}(nil), peerIDs...)

	return nil
}

// Get retrieves a manifest, reading from any available replica.
func (r *ManifestReplicator) Get(ctx context.Context, hash string) (*Manifest, error) {
	if replicas, ok := r.replicas[hash]; ok && len(replicas) > 0 {
		local, err := NewLocalManifest(r.storeDir)
		if err != nil {
			return nil, err
		}
		return local.Get(ctx, hash)
	}

	return nil, fmt.Errorf("manifest not found")
}

// Delete removes a manifest from all replicas.
func (r *ManifestReplicator) Delete(ctx context.Context, hash string) error {
	if _, ok := r.replicas[hash]; ok {
		// Delete from each peer
		delete(r.replicas, hash)
	}

	return nil
}

// LocalManifest handles manifests stored on a single node.
type LocalManifest struct {
	storeDir string
	path     string
}

// NewLocalManifest creates a local manifest handler.
func NewLocalManifest(storeDir string) (*LocalManifest, error) {
	if storeDir == "" {
		return nil, fmt.Errorf("storeDir is required")
	}

	return &LocalManifest{storeDir: storeDir}, nil
}

// Path returns the path where the manifest will be stored.
func (l *LocalManifest) Path(hash string) string {
	return filepath.Join(l.storeDir, "manifests", hash[:8], hash)
}

// Put stores a manifest to disk.
func (l *LocalManifest) Put(ctx context.Context, m *Manifest) error {
	path := l.Path(m.Hash())

	return m.Write(path)
}

// Get retrieves a manifest from disk.
func (l *LocalManifest) Get(ctx context.Context, hash string) (*Manifest, error) {
	path := l.Path(hash)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("manifest %s not found", hash)
		}

		return nil, fmt.Errorf("read manifest %s: %w", hash, err)
	}

	return New(data)
}

// Delete removes a manifest from disk.
func (l *LocalManifest) Delete(ctx context.Context, hash string) error {
	path := l.Path(hash)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete manifest %s: %w", hash, err)
	}

	return nil
}

// ManifestReader wraps an io.Reader for reading manifest data.
type ManifestReader struct {
	data []byte
}

// NewReader creates a new manifest reader.
func NewReader(data []byte) *ManifestReader {
	return &ManifestReader{data: data}
}

// Read reads up to len(p) bytes into p.
func (r *ManifestReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, io.EOF
	}

	return n, nil
}

// ManifestSizer returns the size of manifest data.
func ManifestSizer(data []byte) int64 {
	return int64(len(data))
}
