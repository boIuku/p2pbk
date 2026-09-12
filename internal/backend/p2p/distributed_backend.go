package p2p

import (
	"context"
	"errors"
	"github.com/libp2p/go-libp2p/core/routing"
	"sync"
	"time"

	"p2pbk/internal/backend/p2p/dispatcher"
	"p2pbk/internal/backend/p2p/locator"
	"p2pbk/internal/backend/p2p/placement"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/trust/receipts"
)

const shardFetchTimeout = 5 * time.Second

type DistributedBackend struct {
	mu         sync.Mutex
	dispatcher *dispatcher.Dispatcher
	placement  placement.Strategy
	locator    locator.Locator
	trustDB    *localdb.DB
}

type DistributedBackendOption func(*DistributedBackend)

func WithTrustDB(db *localdb.DB) DistributedBackendOption {
	return func(b *DistributedBackend) {
		b.trustDB = db
	}
}

func NewDistributedBackend(dispatcher *dispatcher.Dispatcher, placement placement.Strategy, locator locator.Locator, opts ...DistributedBackendOption) (*DistributedBackend, error) {
	if dispatcher == nil {
		return nil, errors.New("dispatcher is required")
	}
	if placement == nil {
		return nil, errors.New("placement strategy is required")
	}
	if locator == nil {
		return nil, errors.New("locator is required")
	}
	backend := &DistributedBackend{dispatcher: dispatcher, placement: placement, locator: locator}
	for _, opt := range opts {
		opt(backend)
	}
	return backend, nil
}

func (b *DistributedBackend) PutShard(ctx context.Context, req blobserver.PutShardRequest) (blobserver.PutShardResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	assignments, err := b.placement.Place(req.Ref.Index + 1)
	if err != nil {
		return blobserver.PutShardResponse{}, err
	}
	target := assignments[req.Ref.Index].Peer
	resp, err := b.dispatcher.PutShard(ctx, target, req)
	if err != nil {
		if b.trustDB != nil {
			_ = b.trustDB.RecordPeerFailure(target.String(), time.Now().UTC())
		}
		return blobserver.PutShardResponse{}, err
	}

	if err := b.recordLocation(ctx, req.Ref.BlobID, locator.Location{Peer: target, Ref: req.Ref}); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), shardFetchTimeout)
		_, cleanupErr := b.dispatcher.DeleteShard(cleanup, target, blobserver.DeleteShardRequest{Ref: req.Ref})
		cancel()
		if cleanupErr != nil {
			return blobserver.PutShardResponse{}, errors.Join(err, cleanupErr)
		}
		return blobserver.PutShardResponse{}, err
	}
	if b.trustDB != nil {
		receipt := resp.Receipt
		if receipt.Ref == (receipts.ShardRef{}) {
			receipt.Ref = receiptRef(req.Ref)
		}
		if receipt.Hash == "" {
			receipt.Hash = req.Hash
		}
		if receipt.StoredBy == "" {
			receipt.StoredBy = target.String()
		}
		if receipt.IssuedAt.IsZero() {
			receipt.IssuedAt = time.Now().UTC()
		}
		if err := b.trustDB.StoreShardReceipt(localdb.ShardReceiptRecord{
			PeerID:   target.String(),
			Receipt:  receipt,
			StoredAt: receipt.IssuedAt,
		}); err != nil {
			return blobserver.PutShardResponse{}, err
		}
	}
	return resp, nil
}

func receiptRef(ref blobserver.ShardRef) receipts.ShardRef {
	return receipts.ShardRef{Owner: ref.Owner, BlobID: ref.BlobID, Index: ref.Index}
}

func (b *DistributedBackend) GetShard(ctx context.Context, req blobserver.GetShardRequest) (blobserver.GetShardResponse, error) {
	locations, err := b.locator.Get(ctx, req.Ref.BlobID)
	if err != nil {
		return blobserver.GetShardResponse{}, err
	}

	var lastErr error
	for _, loc := range locator.FilterByRef(locations, req.Ref) {
		fetchCtx, cancel := context.WithTimeout(ctx, shardFetchTimeout)
		got, err := b.dispatcher.GetShard(fetchCtx, loc.Peer, req)
		cancel()
		if err == nil {
			return got, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return blobserver.GetShardResponse{}, lastErr
	}
	return blobserver.GetShardResponse{}, blobserver.ErrShardNotFound
}

func (b *DistributedBackend) DeleteShard(ctx context.Context, req blobserver.DeleteShardRequest) (blobserver.DeleteShardResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	locations, err := b.locator.Get(ctx, req.Ref.BlobID)
	if err != nil {
		return blobserver.DeleteShardResponse{}, err
	}

	deleted := false
	remaining := make([]locator.Location, 0, len(locations))
	var firstErr error
	for _, loc := range locations {
		if loc.Ref != req.Ref {
			remaining = append(remaining, loc)
			continue
		}
		resp, err := b.dispatcher.DeleteShard(ctx, loc.Peer, req)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		deleted = deleted || resp.Deleted
	}
	if firstErr != nil {
		return blobserver.DeleteShardResponse{}, firstErr
	}
	if err := b.locator.Put(ctx, req.Ref.BlobID, remaining); err != nil {
		return blobserver.DeleteShardResponse{}, err
	}
	return blobserver.DeleteShardResponse{Deleted: deleted}, nil
}

func (b *DistributedBackend) recordLocation(ctx context.Context, blobID string, location locator.Location) error {
	locations, err := b.locator.Get(ctx, blobID)
	if errors.Is(err, routing.ErrNotFound) {
		locations = nil
	} else if err != nil {
		return err
	}
	locations = upsertLocation(locations, location)
	return b.locator.Put(ctx, blobID, locations)
}

func upsertLocation(locations []locator.Location, location locator.Location) []locator.Location {
	out := make([]locator.Location, 0, len(locations)+1)
	replaced := false
	for _, existing := range locations {
		if existing.Ref == location.Ref {
			out = append(out, location)
			replaced = true
			continue
		}
		out = append(out, existing)
	}
	if !replaced {
		out = append(out, location)
	}
	return out
}
