package dispatcher

import (
	"github.com/libp2p/go-libp2p/core/peer"

	"p2pbk/internal/peer/blobserver"
)

type UploadResult struct {
	Peer peer.ID
	Ref  blobserver.ShardRef
	Err  error
}

type DownloadResult struct {
	Peer  peer.ID
	Shard blobserver.GetShardResponse
	Err   error
}
