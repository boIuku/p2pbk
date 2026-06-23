package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"sync"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
)

type Dispatcher struct {
	client wire.Client
}

func New(h corehost.Host) *Dispatcher {
	return &Dispatcher{client: wire.NewClient(h)}
}

func (d *Dispatcher) PutShard(ctx context.Context, target peer.ID, req blobserver.PutShardRequest) (blobserver.PutShardResponse, error) {
	if err := req.Ref.Validate(); err != nil {
		return blobserver.PutShardResponse{}, err
	}
	if req.Hash == "" {
		req.Hash = blobserver.Hash(req.Data)
	}
	var out blobserver.PutShardResponse
	if err := d.client.Call(ctx, target, blobserver.MethodPutShard, req, &out); err != nil {
		return blobserver.PutShardResponse{}, fmt.Errorf("put shard on %s: %w", target, err)
	}
	return out, nil
}

func (d *Dispatcher) GetShard(ctx context.Context, target peer.ID, req blobserver.GetShardRequest) (blobserver.GetShardResponse, error) {
	var out blobserver.GetShardResponse
	if err := d.client.Call(ctx, target, blobserver.MethodGetShard, req, &out); err != nil {
		return blobserver.GetShardResponse{}, fmt.Errorf("get shard from %s: %w", target, err)
	}
	if out.Hash != blobserver.Hash(out.Data) {
		return blobserver.GetShardResponse{}, errors.New("downloaded shard hash mismatch")
	}
	return out, nil
}

func (d *Dispatcher) DeleteShard(ctx context.Context, target peer.ID, req blobserver.DeleteShardRequest) (blobserver.DeleteShardResponse, error) {
	var out blobserver.DeleteShardResponse
	if err := d.client.Call(ctx, target, blobserver.MethodDeleteShard, req, &out); err != nil {
		return blobserver.DeleteShardResponse{}, fmt.Errorf("delete shard on %s: %w", target, err)
	}
	return out, nil
}

func (d *Dispatcher) PutShards(ctx context.Context, targets map[peer.ID]blobserver.PutShardRequest) []UploadResult {
	results := make([]UploadResult, 0, len(targets))
	resultCh := make(chan UploadResult, len(targets))

	var wg sync.WaitGroup
	for target, req := range targets {
		wg.Add(1)
		go func(target peer.ID, req blobserver.PutShardRequest) {
			defer wg.Done()
			_, err := d.PutShard(ctx, target, req)
			resultCh <- UploadResult{Peer: target, Ref: req.Ref, Err: err}
		}(target, req)
	}

	wg.Wait()
	close(resultCh)
	for result := range resultCh {
		results = append(results, result)
	}
	return results
}
