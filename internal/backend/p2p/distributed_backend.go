package p2p

import (
	"context"
	"errors"

	"p2pbk/internal/backend/p2p/dispatcher"
	"p2pbk/internal/backend/p2p/locator"
	"p2pbk/internal/backend/p2p/placement"
	"p2pbk/internal/peer/blobserver"
)

type DistributedBackend struct {
	dispatcher *dispatcher.Dispatcher
	placement  placement.Strategy
	locator    locator.Locator
}

func NewDistributedBackend(dispatcher *dispatcher.Dispatcher, placement placement.Strategy, locator locator.Locator) (*DistributedBackend, error) {
	if dispatcher == nil {
		return nil, errors.New("dispatcher is required")
	}
	if placement == nil {
		return nil, errors.New("placement strategy is required")
	}
	if locator == nil {
		return nil, errors.New("locator is required")
	}
	return &DistributedBackend{dispatcher: dispatcher, placement: placement, locator: locator}, nil
}

func (b *DistributedBackend) PutShard(ctx context.Context, req blobserver.PutShardRequest) error {
	assignments, err := b.placement.Place(req.Ref.Index + 1)
	if err != nil {
		return err
	}
	target := assignments[req.Ref.Index].Peer
	if err := b.dispatcher.PutShard(ctx, target, req); err != nil {
		return err
	}

	return b.recordLocation(ctx, req.Ref.BlobID, locator.Location{Peer: target, Ref: req.Ref})
}

func (b *DistributedBackend) GetShard(ctx context.Context, req blobserver.GetShardRequest) (blobserver.GetShardResponse, error) {
	locations, err := b.locator.Get(ctx, req.Ref.BlobID)
	if err != nil {
		return blobserver.GetShardResponse{}, err
	}

	var lastErr error
	for _, loc := range locator.FilterByRef(locations, req.Ref) {
		got, err := b.dispatcher.GetShard(ctx, loc.Peer, req)
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
	if err != nil {
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
