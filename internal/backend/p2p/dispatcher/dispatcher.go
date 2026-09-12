package dispatcher

import (
	"context"
	"errors"
	"fmt"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/trust/receipts"
)

type Dispatcher struct {
	client wire.Client
	host   corehost.Host
}

func New(h corehost.Host) *Dispatcher {
	return &Dispatcher{client: wire.NewClient(h), host: h}
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
	if out.Receipt.Ref != (receipts.ShardRef{Owner: req.Ref.Owner, BlobID: req.Ref.BlobID, Index: req.Ref.Index}) || out.Receipt.Hash != req.Hash || out.Receipt.StoredBy != target.String() {
		return out, errors.New("receipt does not match shard assignment")
	}
	if err := receipts.VerifyWithKey(d.host.Peerstore().PubKey(target), out.Receipt); err != nil {
		return out, err
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
	if out.Ref != req.Ref {
		return out, errors.New("downloaded shard ref mismatch")
	}
	return out, nil
}

func (d *Dispatcher) Capabilities(ctx context.Context, target peer.ID) (blobserver.Capabilities, error) {
	var out blobserver.Capabilities
	err := d.client.Call(ctx, target, blobserver.MethodCapabilities, nil, &out)
	return out, err
}

func (d *Dispatcher) DeleteShard(ctx context.Context, target peer.ID, req blobserver.DeleteShardRequest) (blobserver.DeleteShardResponse, error) {
	var out blobserver.DeleteShardResponse
	if err := d.client.Call(ctx, target, blobserver.MethodDeleteShard, req, &out); err != nil {
		return blobserver.DeleteShardResponse{}, fmt.Errorf("delete shard on %s: %w", target, err)
	}
	return out, nil
}
