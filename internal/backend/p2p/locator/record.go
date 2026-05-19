package locator

import (
	"github.com/libp2p/go-libp2p/core/peer"

	"p2pbk/internal/peer/blobserver"
)

type Location struct {
	Peer peer.ID
	Ref  blobserver.ShardRef
}

type Record struct {
	BlobID    string
	Locations []Location
}
