package p2p

import (
	"context"
	"errors"
	"p2pbk/internal/backend/p2p/locator"
	"p2pbk/internal/peer/blobserver"
	"testing"
)

type unavailableLocator struct{ putCalls int }

func (l *unavailableLocator) Get(context.Context, string) ([]locator.Location, error) {
	return nil, context.DeadlineExceeded
}
func (l *unavailableLocator) Put(context.Context, string, []locator.Location) error {
	l.putCalls++
	return nil
}
func (l *unavailableLocator) Delete(context.Context, string) error { return nil }

// P0-06: a timeout is not "blob absent". Replacing unknown previous state would
// strand shards already accepted by other peers.
func TestLocatorReadFailureDoesNotEraseLocations(t *testing.T) {
	l := &unavailableLocator{}
	b := &DistributedBackend{locator: l}
	err := b.recordLocation(context.Background(), "blob", locator.Location{Ref: blobserver.ShardRef{Owner: "owner", BlobID: "blob", Index: 1}})
	if !errors.Is(err, context.DeadlineExceeded) || l.putCalls != 0 {
		t.Fatalf("lookup failure overwrote locator: err=%v writes=%d", err, l.putCalls)
	}
}
