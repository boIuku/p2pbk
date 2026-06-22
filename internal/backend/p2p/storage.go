package p2p

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kopia/kopia/repo/blob"

	"p2pbk/internal/backend/p2p/erasure"
	"p2pbk/internal/peer/blobserver"
)

type ShardBackend interface {
	PutShard(context.Context, blobserver.PutShardRequest) error
	GetShard(context.Context, blobserver.GetShardRequest) (blobserver.GetShardResponse, error)
	DeleteShard(context.Context, blobserver.DeleteShardRequest) (blobserver.DeleteShardResponse, error)
}

type Storage struct {
	blob.DefaultProviderImplementation

	mu        sync.RWMutex
	options   Options
	codec     *erasure.Codec
	backend   ShardBackend
	manifests map[blob.ID]storedManifest
	closed    bool
}

type storedManifest struct {
	Metadata blob.Metadata
	Erasure  erasure.Manifest
}

func NewStorage(backend ShardBackend, options Options) (*Storage, error) {
	if backend == nil {
		return nil, errors.New("shard backend is required")
	}
	options = options.withDefaults()

	codec, err := erasure.New(options.DataShards, options.ParityShards)
	if err != nil {
		return nil, err
	}

	s := &Storage{
		options:   options,
		codec:     codec,
		backend:   backend,
		manifests: map[blob.ID]storedManifest{},
	}
	if err := s.loadManifests(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Storage) PutBlob(ctx context.Context, blobID blob.ID, data blob.Bytes, opts blob.PutOptions) error {
	if opts.HasRetentionOptions() {
		return blob.ErrUnsupportedPutBlobOption
	}

	payload, err := readBlobBytes(data)
	if err != nil {
		return fmt.Errorf("read blob bytes: %w", err)
	}

	manifest, shards, err := s.codec.Encode(payload)
	if err != nil {
		return fmt.Errorf("encode blob shards: %w", err)
	}

	modTime := opts.SetModTime
	if modTime.IsZero() {
		modTime = time.Now().UTC()
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("storage is closed")
	}
	if opts.DoNotRecreate {
		if _, ok := s.manifests[blobID]; ok {
			s.mu.Unlock()
			return blob.ErrBlobAlreadyExists
		}
	}
	s.mu.Unlock()

	refs := make([]blobserver.ShardRef, 0, len(shards))
	for _, shard := range shards {
		ref := s.shardRef(blobID, shard.Index)
		refs = append(refs, ref)
		if err := s.backend.PutShard(ctx, blobserver.PutShardRequest{
			Ref:  ref,
			Data: shard.Data,
			Hash: hex.EncodeToString(shard.Hash[:]),
		}); err != nil {
			return fmt.Errorf("put shard %d: %w", shard.Index, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("storage is closed")
	}
	s.manifests[blobID] = storedManifest{
		Metadata: blob.Metadata{BlobID: blobID, Length: int64(len(payload)), Timestamp: modTime},
		Erasure:  manifest,
	}
	if err := s.saveManifestsLocked(); err != nil {
		return err
	}
	if opts.GetModTime != nil {
		*opts.GetModTime = modTime
	}

	return nil
}

func (s *Storage) GetBlob(ctx context.Context, blobID blob.ID, offset, length int64, output blob.OutputBuffer) error {
	manifest, err := s.manifest(blobID)
	if err != nil {
		return err
	}

	if offset < 0 || offset > manifest.Metadata.Length {
		return blob.ErrInvalidRange
	}
	if length >= 0 && offset+length > manifest.Metadata.Length {
		return blob.ErrInvalidRange
	}

	shards := make([]erasure.Shard, 0, len(manifest.Erasure.Shards))
	for _, info := range manifest.Erasure.Shards {
		ref := s.shardRef(blobID, info.Index)
		got, err := s.backend.GetShard(ctx, blobserver.GetShardRequest{Ref: ref})
		if err != nil {
			continue
		}
		shards = append(shards, erasure.Shard{Index: info.Index, Data: got.Data, Hash: info.Hash})
	}

	payload, err := s.codec.Decode(manifest.Erasure, shards)
	if err != nil {
		return fmt.Errorf("decode blob shards: %w", err)
	}

	start := int(offset)
	end := len(payload)
	if length >= 0 {
		end = int(offset + length)
	}
	output.Reset()
	if _, err := output.Write(payload[start:end]); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func (s *Storage) GetMetadata(ctx context.Context, blobID blob.ID) (blob.Metadata, error) {
	manifest, err := s.manifest(blobID)
	if err != nil {
		return blob.Metadata{}, err
	}
	return manifest.Metadata, nil
}

func (s *Storage) ListBlobs(ctx context.Context, prefix blob.ID, cb func(blob.Metadata) error) error {
	s.mu.RLock()
	ids := make([]string, 0, len(s.manifests))
	for id := range s.manifests {
		if strings.HasPrefix(string(id), string(prefix)) {
			ids = append(ids, string(id))
		}
	}
	sort.Strings(ids)

	metadata := make([]blob.Metadata, 0, len(ids))
	for _, id := range ids {
		metadata = append(metadata, s.manifests[blob.ID(id)].Metadata)
	}
	s.mu.RUnlock()

	for _, md := range metadata {
		if err := cb(md); err != nil {
			return err
		}
	}
	return nil
}

func (s *Storage) DeleteBlob(ctx context.Context, blobID blob.ID) error {
	manifest, err := s.manifest(blobID)
	if err != nil {
		return err
	}

	for _, info := range manifest.Erasure.Shards {
		_, err := s.backend.DeleteShard(ctx, blobserver.DeleteShardRequest{Ref: s.shardRef(blobID, info.Index)})
		if err != nil {
			return fmt.Errorf("delete shard %d: %w", info.Index, err)
		}
	}

	s.mu.Lock()
	delete(s.manifests, blobID)
	if err := s.saveManifestsLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return nil
}

func (s *Storage) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *Storage) FlushCaches(ctx context.Context) error {
	return nil
}

func (s *Storage) ConnectionInfo() blob.ConnectionInfo {
	return blob.ConnectionInfo{
		Type: "p2pbk",
		Config: map[string]any{
			"owner":          s.options.Owner,
			"dataShards":     s.options.DataShards,
			"parityShards":   s.options.ParityShards,
			"backend":        "shards",
			"implementation": "experimental",
		},
	}
}

func (s *Storage) DisplayName() string {
	return s.options.DisplayName
}

func (s *Storage) manifest(blobID blob.ID) (storedManifest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return storedManifest{}, errors.New("storage is closed")
	}
	manifest, ok := s.manifests[blobID]
	if !ok {
		return storedManifest{}, blob.ErrBlobNotFound
	}
	return manifest, nil
}

func (s *Storage) shardRef(blobID blob.ID, index int) blobserver.ShardRef {
	return blobserver.ShardRef{
		Owner:  s.options.Owner,
		BlobID: hex.EncodeToString([]byte(blobID)),
		Index:  index,
	}
}

func readBlobBytes(data blob.Bytes) ([]byte, error) {
	var out bytes.Buffer
	if _, err := data.WriteTo(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (s *Storage) loadManifests() error {
	if s.options.ManifestPath == "" {
		return nil
	}
	data, err := os.ReadFile(s.options.ManifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read manifest index: %w", err)
	}

	var raw map[string]storedManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode manifest index: %w", err)
	}
	for id, manifest := range raw {
		manifest.Metadata.BlobID = blob.ID(id)
		s.manifests[blob.ID(id)] = manifest
	}
	return nil
}

func (s *Storage) saveManifestsLocked() error {
	if s.options.ManifestPath == "" {
		return nil
	}
	raw := make(map[string]storedManifest, len(s.manifests))
	for id, manifest := range s.manifests {
		raw[string(id)] = manifest
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest index: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.options.ManifestPath), 0o700); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.options.ManifestPath), "manifests-*.tmp")
	if err != nil {
		return fmt.Errorf("create manifest temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write manifest temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close manifest temp file: %w", err)
	}
	if err := os.Rename(tmpPath, s.options.ManifestPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace manifest index: %w", err)
	}
	return nil
}

var _ blob.Storage = (*Storage)(nil)
