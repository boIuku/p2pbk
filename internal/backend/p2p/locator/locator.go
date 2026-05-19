package locator

import (
	"context"
	"sort"
	"sync"

	"p2pbk/internal/peer/blobserver"
)

type Locator interface {
	Put(context.Context, string, []Location) error
	Get(context.Context, string) ([]Location, error)
	Delete(context.Context, string) error
}

type Memory struct {
	mu      sync.RWMutex
	records map[string][]Location
}

func NewMemory() *Memory {
	return &Memory{records: map[string][]Location{}}
}

func (m *Memory) Put(ctx context.Context, blobID string, locations []Location) error {
	copyLocations := append([]Location(nil), locations...)
	sortLocations(copyLocations)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[blobID] = copyLocations
	return nil
}

func (m *Memory) Get(ctx context.Context, blobID string) ([]Location, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Location(nil), m.records[blobID]...), nil
}

func (m *Memory) Delete(ctx context.Context, blobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, blobID)
	return nil
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
