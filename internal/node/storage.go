package node

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"p2pbk/internal/backend/p2p"
	"p2pbk/internal/backend/p2p/dispatcher"
	"p2pbk/internal/backend/p2p/locator"
	"p2pbk/internal/backend/p2p/placement"
	"p2pbk/internal/config"
	p2pdht "p2pbk/internal/network/dht"
	p2pdiscovery "p2pbk/internal/network/discovery"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/trust/identity"
)

type DistributedStorage struct {
	Storage *p2p.Storage
	DHT     *p2pdht.DHT
	Peers   []peer.ID
	DB      *localdb.DB
}

func OpenDistributedStorage(ctx context.Context, cfg config.Config, h corehost.Host, id *identity.Identity) (*DistributedStorage, error) {
	if id == nil {
		return nil, errors.New("identity is required for distributed storage")
	}
	store, closeStore, err := openPeerStore(cfg)
	if err != nil {
		if !localdb.IsOpenTimeout(err) {
			return nil, err
		}
		store = nil
		closeStore = func() {}
	}

	infrastructureFilter, err := publicDiscoveryFilter(cfg)
	if err != nil {
		closeStore()
		return nil, err
	}
	if _, err := p2pdiscovery.ConnectConfiguredAndSaved(ctx, h, cfg.Network.PublicDiscoveryPeers, nil); err != nil {
		closeStore()
		return nil, err
	}
	if _, err := p2pdiscovery.ConnectConfiguredAndSaved(ctx, h, cfg.Network.BootstrapPeers, store, p2pdiscovery.WithIgnoredPeers(infrastructureFilter)); err != nil {
		closeStore()
		return nil, err
	}
	if err := p2pdiscovery.SaveConnectedPeers(store, h, "storage", p2pdiscovery.WithIgnoredPeers(infrastructureFilter)); err != nil {
		closeStore()
		return nil, err
	}

	dht, err := p2pdht.New(ctx, h, p2pdht.ModeClient)
	if err != nil {
		closeStore()
		return nil, err
	}
	if err := dht.Bootstrap(ctx); err != nil {
		_ = dht.Close()
		closeStore()
		return nil, err
	}

	loc, err := locator.NewDHT(dht, id, id.PublicKey())
	if err != nil {
		_ = dht.Close()
		closeStore()
		return nil, err
	}
	st, peers, err := openDistributedStorageWithConnectedPeers(cfg, h, loc, store, infrastructureFilter)
	if err != nil {
		_ = dht.Close()
		closeStore()
		return nil, err
	}

	return &DistributedStorage{Storage: st, DHT: dht, Peers: peers, DB: store}, nil
}

func OpenDistributedStorageWithLocator(ctx context.Context, cfg config.Config, h corehost.Host, loc locator.Locator) (*p2p.Storage, []peer.ID, error) {
	if loc == nil {
		return nil, nil, errors.New("locator is required")
	}

	store, closeStore, err := openPeerStore(cfg)
	if err != nil {
		if !localdb.IsOpenTimeout(err) {
			return nil, nil, err
		}
		store = nil
		closeStore = func() {}
	}
	defer closeStore()

	infrastructureFilter, err := publicDiscoveryFilter(cfg)
	if err != nil {
		return nil, nil, err
	}
	if _, err := p2pdiscovery.ConnectConfiguredAndSaved(ctx, h, cfg.Network.PublicDiscoveryPeers, nil); err != nil {
		return nil, nil, err
	}
	if _, err := p2pdiscovery.ConnectConfiguredAndSaved(ctx, h, cfg.Network.BootstrapPeers, store, p2pdiscovery.WithIgnoredPeers(infrastructureFilter)); err != nil {
		return nil, nil, err
	}
	if err := p2pdiscovery.SaveConnectedPeers(store, h, "storage", p2pdiscovery.WithIgnoredPeers(infrastructureFilter)); err != nil {
		return nil, nil, err
	}

	return openDistributedStorageWithConnectedPeers(cfg, h, loc, nil, infrastructureFilter)
}

func openDistributedStorageWithConnectedPeers(cfg config.Config, h corehost.Host, loc locator.Locator, store *localdb.DB, filter p2pdiscovery.PeerFilter) (*p2p.Storage, []peer.ID, error) {
	peers := connectedStoragePeers(h, filter)
	minPeers := cfg.Network.MinPeers
	if minPeers == 0 {
		minPeers = 1
	}
	if len(peers) < minPeers {
		return nil, nil, fmt.Errorf("connected peers %d below network.min_peers %d", len(peers), minPeers)
	}

	strategy, err := placement.NewScoredRoundRobin(peers, peerScores(store))
	if err != nil {
		return nil, nil, err
	}
	backend, err := p2p.NewDistributedBackend(dispatcher.New(h), strategy, loc, p2p.WithTrustDB(store))
	if err != nil {
		return nil, nil, err
	}
	paths, err := cfg.Paths()
	if err != nil {
		return nil, nil, err
	}
	st, err := p2p.NewStorage(backend, p2p.Options{
		Owner:        owner(cfg),
		DataShards:   cfg.Backup.DataShards,
		ParityShards: cfg.Backup.ParityShards,
		DisplayName:  "p2pbk distributed storage",
		ManifestPath: filepath.Join(paths.DataDir, "manifests.json"),
	})
	if err != nil {
		return nil, nil, err
	}
	return st, peers, nil
}

func peerScores(store *localdb.DB) map[peer.ID]float64 {
	out := map[peer.ID]float64{}
	if store == nil {
		return out
	}
	stats, err := store.ListPeerStats()
	if err != nil {
		return out
	}
	for _, stat := range stats {
		id, err := peer.Decode(stat.ID)
		if err != nil {
			continue
		}
		out[id] = stat.Score
	}
	return out
}

func (d *DistributedStorage) Close(ctx context.Context) error {
	var out error
	if d.Storage != nil {
		out = errors.Join(out, d.Storage.Close(ctx))
	}
	if d.DHT != nil {
		out = errors.Join(out, d.DHT.Close())
	}
	if d.DB != nil {
		out = errors.Join(out, d.DB.Close())
	}
	return out
}

func connectedStoragePeers(h corehost.Host, filter p2pdiscovery.PeerFilter) []peer.ID {
	uniq := map[peer.ID]struct{}{}
	for _, id := range h.Network().Peers() {
		if id == "" || id == h.ID() || filter.Ignored(id) {
			continue
		}
		uniq[id] = struct{}{}
	}
	out := make([]peer.ID, 0, len(uniq))
	for id := range uniq {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func owner(cfg config.Config) string {
	if cfg.Node.DisplayName != "" {
		return cfg.Node.DisplayName
	}
	return "local"
}

func publicDiscoveryFilter(cfg config.Config) (p2pdiscovery.PeerFilter, error) {
	infos, err := p2phost.ParseAddrInfos(cfg.Network.PublicDiscoveryPeers)
	if err != nil {
		return p2pdiscovery.PeerFilter{}, fmt.Errorf("parse public discovery peer: %w", err)
	}
	return p2pdiscovery.NewPeerFilter(infos), nil
}

func openPeerStore(cfg config.Config) (*localdb.DB, func(), error) {
	paths, err := cfg.Paths()
	if err != nil {
		return nil, func() {}, err
	}
	db, err := localdb.Open(paths.LocalDB)
	if err != nil {
		return nil, func() {}, err
	}
	return db, func() { _ = db.Close() }, nil
}
