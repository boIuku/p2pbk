package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

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
		return nil, err
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

	paths, err := cfg.Paths()
	if err != nil {
		_ = dht.Close()
		closeStore()
		return nil, err
	}
	if cfg.Network.Discovery == "remote" && len(cfg.Network.PublicDiscoveryPeers) > 0 && len(connectedStoragePeers(h, infrastructureFilter)) < cfg.Network.MinPeers {
		discoveryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		svc, startErr := p2pdiscovery.StartRemote(discoveryCtx, h, store, dht, cfg.Network.RemoteDiscoveryNamespace, p2pdiscovery.WithRemoteInterval(time.Second), p2pdiscovery.WithRemoteIgnoredPeers(infrastructureFilter))
		if startErr == nil {
			ticker := time.NewTicker(50 * time.Millisecond)
			for len(connectedStoragePeers(h, infrastructureFilter)) < cfg.Network.MinPeers && discoveryCtx.Err() == nil {
				select {
				case <-ticker.C:
				case <-discoveryCtx.Done():
				}
			}
			ticker.Stop()
			cancel()
			_ = svc.Close()
		} else {
			cancel()
		}
	}
	if _, err := os.Stat(filepath.Join(paths.DataDir, "kopia.repository.config")); err == nil {
		if _, err := os.Stat(filepath.Join(paths.DataDir, "locators.json")); err != nil {
			_ = dht.Close()
			closeStore()
			return nil, fmt.Errorf("recovery locator metadata missing: %w", err)
		}
	}
	loc, err := locator.NewPersistentDHT(dht, id, filepath.Join(paths.DataDir, "locators.json"))
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
		return nil, nil, err
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
	dispatch := dispatcher.New(h)
	var writable []peer.ID
	for _, p := range peers {
		probe, cancel := context.WithTimeout(context.Background(), time.Second)
		cap, err := dispatch.Capabilities(probe, p)
		cancel()
		if err == nil && cap.Writable {
			writable = append(writable, p)
		}
	}
	var strategy placement.Strategy = unavailablePlacement{have: len(writable), want: minPeers}
	if len(writable) >= minPeers {
		scores := peerScores(store)
		domains := peerFailureDomains(store)
		if len(domains) > 0 {
			candidate, domainErr := placement.NewFailureDomainRoundRobin(writable, domains, cfg.Backup.ParityShards)
			if domainErr == nil {
				strategy = candidate
			} else {
				strategy = unavailablePlacement{have: len(domains), want: (cfg.Backup.DataShards + cfg.Backup.ParityShards + cfg.Backup.ParityShards - 1) / cfg.Backup.ParityShards}
			}
		} else {
			strategy, _ = placement.NewScoredRoundRobin(writable, scores)
		}
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
		Owner:        h.ID().String(),
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

type unavailablePlacement struct{ have, want int }

func (p unavailablePlacement) Place(int) ([]placement.Assignment, error) {
	return nil, fmt.Errorf("insufficient writable storage peers: connected %d, require %d", p.have, p.want)
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

func peerFailureDomains(store *localdb.DB) map[peer.ID]string {
	out := map[peer.ID]string{}
	if store == nil {
		return out
	}
	approvals, err := store.ListApprovals()
	if err != nil {
		return out
	}
	for _, approval := range approvals {
		if approval.FailureDomain == "" || approval.RevokedAt.After(approval.ApprovedAt) {
			continue
		}
		id, err := peer.Decode(approval.PeerID)
		if err == nil {
			out[id] = approval.FailureDomain
		}
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
