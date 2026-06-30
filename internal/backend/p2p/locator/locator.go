package locator

import (
	"context"
	"sort"

	"p2pbk/internal/peer/blobserver"
)

type Locator interface {
	Put(context.Context, string, []Location) error
	Get(context.Context, string) ([]Location, error)
	Delete(context.Context, string) error
}

func sortLocations(locations []Location) {
	sort.Slice(locations, func(i, j int) bool {
		if locations[i].Ref.Index == locations[j].Ref.Index {
			return locations[i].Peer.String() < locations[j].Peer.String()
		}
		return locations[i].Ref.Index < locations[j].Ref.Index
	})
}

func FilterByRef(locations []Location, ref blobserver.ShardRef) []Location {
	out := make([]Location, 0, len(locations))
	for _, loc := range locations {
		if loc.Ref == ref {
			out = append(out, loc)
		}
	}
	return out
}
