package p2p

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gofrs/flock"
	"os"
	"p2pbk/internal/durable"
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
	PutShard(context.Context, blobserver.PutShardRequest) (blobserver.PutShardResponse, error)
	GetShard(context.Context, blobserver.GetShardRequest) (blobserver.GetShardResponse, error)
	DeleteShard(context.Context, blobserver.DeleteShardRequest) (blobserver.DeleteShardResponse, error)
}

type Storage struct {
	blob.DefaultProviderImplementation

	mu        sync.RWMutex
	opMu      sync.Mutex
	pending   []blobserver.ShardRef
	writeFile func(string, []byte, os.FileMode) error
	options   Options
	codec     *erasure.Codec
	backend   ShardBackend
	manifests map[blob.ID]storedManifest
	closed    bool
}

type storedManifest struct {
	ShardBlobID string
	Metadata    blob.Metadata
	Erasure     erasure.Manifest
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
		writeFile: durable.WriteFile,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	unlock, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	_ = s.cleanPending(ctx)
	return s, nil
}

func (s *Storage) PutBlob(ctx context.Context, blobID blob.ID, data blob.Bytes, opts blob.PutOptions) error {
	unlock, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer unlock()
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

	old, existed := s.manifests[blobID]
	shardID := hex.EncodeToString([]byte(blobID))
	if existed {
		sum := sha256.Sum256(payload)
		shardID += "-" + hex.EncodeToString(sum[:])
	}
	// Stage immutable generations and journal all cleanup work before touching peers.
	// Pending refs still referenced by a committed manifest are never removed.
	for _, shard := range shards {
		s.pending = append(s.pending, blobserver.ShardRef{Owner: s.options.Owner, BlobID: shardID, Index: shard.Index})
	}
	if existed {
		s.pending = append(s.pending, s.refs(blobID, old)...)
	}
	if err := s.savePending(); err != nil {
		return err
	}
	for _, shard := range shards {
		ref := blobserver.ShardRef{Owner: s.options.Owner, BlobID: shardID, Index: shard.Index}
		if _, err := s.backend.PutShard(ctx, blobserver.PutShardRequest{
			Ref:  ref,
			Data: shard.Data,
			Hash: hex.EncodeToString(shard.Hash[:]),
		}); err != nil {
			_ = s.cleanPending(ctx)
			return fmt.Errorf("put shard %d: %w", shard.Index, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("storage is closed")
	}
	s.manifests[blobID] = storedManifest{
		ShardBlobID: shardID,
		Metadata:    blob.Metadata{BlobID: blobID, Length: int64(len(payload)), Timestamp: modTime},
		Erasure:     manifest,
	}
	if err := s.saveManifestsLocked(); err != nil {
		if existed {
			s.manifests[blobID] = old
		} else {
			delete(s.manifests, blobID)
		}
		return err
	}
	_ = s.cleanPending(ctx)
	if opts.GetModTime != nil {
		*opts.GetModTime = modTime
	}

	return nil
}

func (s *Storage) GetBlob(ctx context.Context, blobID blob.ID, offset, length int64, output blob.OutputBuffer) error {
	unlock, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	manifest, err := s.manifest(blobID)
	if err != nil {
		return err
	}

	if offset < 0 || offset > manifest.Metadata.Length {
		return blob.ErrInvalidRange
	}
	if length >= 0 && length > manifest.Metadata.Length-offset {
		return blob.ErrInvalidRange
	}

	shards := make([]erasure.Shard, 0, len(manifest.Erasure.Shards))
	codec, err := erasure.New(manifest.Erasure.DataShards, manifest.Erasure.ParityShards)
	if err != nil {
		return err
	}
	for _, info := range manifest.Erasure.Shards {
		if err := ctx.Err(); err != nil {
			return err
		}
		ref := s.ref(blobID, manifest, info.Index)
		got, err := s.backend.GetShard(ctx, blobserver.GetShardRequest{Ref: ref})
		if err != nil {
			continue
		}
		if len(got.Data) != manifest.Erasure.ShardSize || sha256.Sum256(got.Data) != info.Hash {
			continue
		}
		shards = append(shards, erasure.Shard{Index: info.Index, Data: got.Data, Hash: info.Hash})
		if len(shards) >= manifest.Erasure.DataShards {
			break
		}
	}

	payload, err := codec.Decode(manifest.Erasure, shards)
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
	unlock, err := s.begin(ctx)
	if err != nil {
		return blob.Metadata{}, err
	}
	defer unlock()
	manifest, err := s.manifest(blobID)
	if err != nil {
		return blob.Metadata{}, err
	}
	return manifest.Metadata, nil
}

func (s *Storage) ListBlobs(ctx context.Context, prefix blob.ID, cb func(blob.Metadata) error) error {
	unlock, err := s.begin(ctx)
	if err != nil {
		return err
	}
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
	unlock()

	for _, md := range metadata {
		if err := cb(md); err != nil {
			return err
		}
	}
	return nil
}

func (s *Storage) DeleteBlob(ctx context.Context, blobID blob.ID) error {
	unlock, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	manifest, err := s.manifest(blobID)
	if err != nil {
		return err
	}

	s.pending = append(s.pending, s.refs(blobID, manifest)...)
	if err := s.savePending(); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.manifests, blobID)
	if err := s.saveManifestsLocked(); err != nil {
		s.manifests[blobID] = manifest
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	_ = s.cleanPending(ctx)
	return nil
}

func (s *Storage) Close(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
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
		s.manifests = map[blob.ID]storedManifest{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read manifest index: %w", err)
	}

	var raw map[string]storedManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode manifest index: %w", err)
	}
	s.manifests = map[blob.ID]storedManifest{}
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
	return s.writeFile(s.options.ManifestPath, data, 0600)
}

func (s *Storage) ref(id blob.ID, m storedManifest, index int) blobserver.ShardRef {
	r := s.shardRef(id, index)
	if m.ShardBlobID != "" {
		r.BlobID = m.ShardBlobID
	}
	return r
}
func (s *Storage) refs(id blob.ID, m storedManifest) []blobserver.ShardRef {
	refs := make([]blobserver.ShardRef, 0, len(m.Erasure.Shards))
	for _, info := range m.Erasure.Shards {
		refs = append(refs, s.ref(id, m, info.Index))
	}
	return refs
}

// begin serializes processes as well as goroutines and reloads committed state.
// This is deliberately one metadata transaction at a time until a transactional
// database replaces the JSON index. A mutex alone cannot protect two CLI processes.
func (s *Storage) begin(ctx context.Context) (func(), error) {
	s.opMu.Lock()
	if ctx.Err() != nil {
		s.opMu.Unlock()
		return nil, ctx.Err()
	}
	var lock *flock.Flock
	fail := func(err error) (func(), error) {
		if lock != nil {
			_ = lock.Unlock()
		}
		s.opMu.Unlock()
		return nil, err
	}
	if s.options.ManifestPath != "" {
		if err := os.MkdirAll(filepath.Dir(s.options.ManifestPath), 0700); err != nil {
			return fail(err)
		}
		lock = flock.New(s.options.ManifestPath + ".lock")
		ok, err := lock.TryLockContext(ctx, 10*time.Millisecond)
		if err != nil {
			return fail(err)
		}
		if !ok {
			return fail(errors.New("metadata locked"))
		}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fail(errors.New("storage is closed"))
	}
	err := s.loadManifests()
	s.mu.Unlock()
	if err != nil {
		return fail(err)
	}
	if s.options.ManifestPath != "" {
		data, err := os.ReadFile(s.options.ManifestPath + ".pending")
		if err == nil {
			if err = json.Unmarshal(data, &s.pending); err != nil {
				return fail(err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
	}
	return func() {
		if lock != nil {
			_ = lock.Unlock()
		}
		s.opMu.Unlock()
	}, nil
}

func (s *Storage) savePending() error {
	if s.options.ManifestPath == "" {
		return nil
	}
	data, err := json.Marshal(s.pending)
	if err != nil {
		return err
	}
	return s.writeFile(s.options.ManifestPath+".pending", data, 0600)
}
func (s *Storage) cleanPending(ctx context.Context) error {
	active := map[blobserver.ShardRef]bool{}
	for id, m := range s.manifests {
		for _, r := range s.refs(id, m) {
			active[r] = true
		}
	}
	remaining := []blobserver.ShardRef{}
	seen := map[blobserver.ShardRef]bool{}
	for _, ref := range s.pending {
		if active[ref] || seen[ref] {
			continue
		}
		seen[ref] = true
		cleanup, cancel := context.WithTimeout(ctx, time.Second)
		_, err := s.backend.DeleteShard(cleanup, blobserver.DeleteShardRequest{Ref: ref})
		cancel()
		if err != nil {
			remaining = append(remaining, ref)
		}
	}
	s.pending = remaining
	return s.savePending()
}

var _ blob.Storage = (*Storage)(nil)
