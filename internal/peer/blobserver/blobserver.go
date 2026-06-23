package blobserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/quota"
	"p2pbk/internal/trust/identity"
	"p2pbk/internal/trust/receipts"
)

const (
	MethodPutShard    = "blobserver.put_shard"
	MethodGetShard    = "blobserver.get_shard"
	MethodDeleteShard = "blobserver.delete_shard"
)

var ErrShardNotFound = errors.New("shard not found")

type Server struct {
	root     string
	quota    *quota.Manager
	identity *identity.Identity
	peerID   string
}

type PutShardRequest struct {
	Ref  ShardRef `json:"ref"`
	Data []byte   `json:"data"`
	Hash string   `json:"hash"`
}

type PutShardResponse struct {
	Receipt receipts.Receipt `json:"receipt"`
}

type GetShardRequest struct {
	Ref ShardRef `json:"ref"`
}

type GetShardResponse struct {
	Ref  ShardRef `json:"ref"`
	Data []byte   `json:"data"`
	Hash string   `json:"hash"`
}

type DeleteShardRequest struct {
	Ref ShardRef `json:"ref"`
}

type DeleteShardResponse struct {
	Deleted bool `json:"deleted"`
}

func New(root string) (*Server, error) {
	return NewWithQuota(root, nil)
}

func NewWithQuota(root string, quota *quota.Manager) (*Server, error) {
	return NewWithOptions(root, Options{Quota: quota})
}

type Options struct {
	Quota    *quota.Manager
	Identity *identity.Identity
	PeerID   string
}

func NewWithOptions(root string, options Options) (*Server, error) {
	if root == "" {
		return nil, errors.New("blobserver root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create blobserver root: %w", err)
	}
	return &Server{root: root, quota: options.Quota, identity: options.Identity, peerID: options.PeerID}, nil
}

func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Server) PutShard(ctx context.Context, req PutShardRequest) (PutShardResponse, error) {
	if err := req.Ref.Validate(); err != nil {
		return PutShardResponse{}, err
	}
	if req.Hash == "" {
		req.Hash = Hash(req.Data)
	}
	if got := Hash(req.Data); got != req.Hash {
		return PutShardResponse{}, fmt.Errorf("shard hash mismatch: got %s want %s", got, req.Hash)
	}

	path, err := ShardPath(s.root, req.Ref)
	if err != nil {
		return PutShardResponse{}, err
	}

	var previousSize int64
	if info, err := os.Stat(path); err == nil {
		previousSize = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return PutShardResponse{}, fmt.Errorf("stat existing shard: %w", err)
	}
	delta := int64(len(req.Data)) - previousSize
	if s.quota != nil && delta > 0 {
		if err := s.quota.Reserve(req.Ref.Owner, delta); err != nil {
			return PutShardResponse{}, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		if s.quota != nil && delta > 0 {
			s.quota.Release(req.Ref.Owner, delta)
		}
		return PutShardResponse{}, fmt.Errorf("create shard directory: %w", err)
	}
	if err := os.WriteFile(path, req.Data, 0o600); err != nil {
		if s.quota != nil && delta > 0 {
			s.quota.Release(req.Ref.Owner, delta)
		}
		return PutShardResponse{}, fmt.Errorf("write shard: %w", err)
	}
	if s.quota != nil && delta < 0 {
		s.quota.Release(req.Ref.Owner, -delta)
	}

	receipt, err := s.issueReceipt(req.Ref, req.Hash)
	if err != nil {
		return PutShardResponse{}, err
	}
	return PutShardResponse{Receipt: receipt}, nil
}

func (s *Server) GetShard(ctx context.Context, req GetShardRequest) (GetShardResponse, error) {
	path, err := ShardPath(s.root, req.Ref)
	if err != nil {
		return GetShardResponse{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return GetShardResponse{}, ErrShardNotFound
	}
	if err != nil {
		return GetShardResponse{}, fmt.Errorf("read shard: %w", err)
	}
	return GetShardResponse{Ref: req.Ref, Data: data, Hash: Hash(data)}, nil
}

func (s *Server) DeleteShard(ctx context.Context, req DeleteShardRequest) (DeleteShardResponse, error) {
	path, err := ShardPath(s.root, req.Ref)
	if err != nil {
		return DeleteShardResponse{}, err
	}
	var size int64
	if info, err := os.Stat(path); err == nil {
		size = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return DeleteShardResponse{}, fmt.Errorf("stat shard: %w", err)
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return DeleteShardResponse{Deleted: false}, nil
	} else if err != nil {
		return DeleteShardResponse{}, fmt.Errorf("delete shard: %w", err)
	}
	if s.quota != nil {
		s.quota.Release(req.Ref.Owner, size)
	}
	return DeleteShardResponse{Deleted: true}, nil
}

func (s *Server) Register(router *wire.Router) {
	router.Handle(MethodPutShard, s.handlePutShard)
	router.Handle(MethodGetShard, s.handleGetShard)
	router.Handle(MethodDeleteShard, s.handleDeleteShard)
}

func (s *Server) handlePutShard(ctx context.Context, req wire.Request) (wire.Response, error) {
	var in PutShardRequest
	if err := json.Unmarshal(req.Payload, &in); err != nil {
		return wire.Response{}, err
	}
	out, err := s.PutShard(ctx, in)
	if err != nil {
		return wire.Response{}, err
	}
	return wire.OK(out)
}

func (s *Server) handleGetShard(ctx context.Context, req wire.Request) (wire.Response, error) {
	var in GetShardRequest
	if err := json.Unmarshal(req.Payload, &in); err != nil {
		return wire.Response{}, err
	}
	out, err := s.GetShard(ctx, in)
	if err != nil {
		return wire.Response{}, err
	}
	return wire.OK(out)
}

func (s *Server) issueReceipt(ref ShardRef, hash string) (receipts.Receipt, error) {
	if s.identity == nil {
		return receipts.Receipt{
			Ref:      receiptRef(ref),
			Hash:     hash,
			StoredBy: s.peerID,
			IssuedAt: time.Now().UTC(),
		}, nil
	}
	storedBy := s.peerID
	if storedBy == "" {
		storedBy = s.identity.Fingerprint()
	}
	return receipts.Issue(s.identity, receiptRef(ref), hash, storedBy, time.Now().UTC())
}

func receiptRef(ref ShardRef) receipts.ShardRef {
	return receipts.ShardRef{Owner: ref.Owner, BlobID: ref.BlobID, Index: ref.Index}
}

func (s *Server) handleDeleteShard(ctx context.Context, req wire.Request) (wire.Response, error) {
	var in DeleteShardRequest
	if err := json.Unmarshal(req.Payload, &in); err != nil {
		return wire.Response{}, err
	}
	out, err := s.DeleteShard(ctx, in)
	if err != nil {
		return wire.Response{}, err
	}
	return wire.OK(out)
}
