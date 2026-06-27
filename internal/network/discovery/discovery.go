package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/multiformats/go-multiaddr"

	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/peer/localdb"
)

type PeerStore interface {
	UpsertPeer(localdb.PeerRecord) error
	RecordPeerFailure(string, time.Time) error
	ListPeers() ([]localdb.PeerRecord, error)
}

type LocalService struct {
	service mdns.Service
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

type EventType string

const (
	EventFound         EventType = "found"
	EventConnected     EventType = "connected"
	EventSaved         EventType = "saved"
	EventConnectFailed EventType = "connect_failed"
	EventSaveFailed    EventType = "save_failed"
)

type Event struct {
	Type   EventType
	PeerID string
	Addrs  []string
	Source string
	Err    error
}

type PeerFilter struct {
	ignored map[peer.ID]struct{}
}

func NewPeerFilter(infos []peer.AddrInfo) PeerFilter {
	filter := PeerFilter{ignored: map[peer.ID]struct{}{}}
	for _, info := range infos {
		if info.ID != "" {
			filter.ignored[info.ID] = struct{}{}
		}
	}
	return filter
}

func (f PeerFilter) Ignored(id peer.ID) bool {
	if id == "" {
		return true
	}
	_, ok := f.ignored[id]
	return ok
}

func (f PeerFilter) IDs() []peer.ID {
	out := make([]peer.ID, 0, len(f.ignored))
	for id := range f.ignored {
		out = append(out, id)
	}
	return out
}

type connectOptions struct {
	filter PeerFilter
}

type ConnectOption func(*connectOptions)

func WithIgnoredPeers(filter PeerFilter) ConnectOption {
	return func(opts *connectOptions) {
		opts.filter = filter
	}
}

type localOptions struct {
	observer func(Event)
}

type LocalOption func(*localOptions)

func WithObserver(observer func(Event)) LocalOption {
	return func(opts *localOptions) {
		opts.observer = observer
	}
}

func StartLocal(ctx context.Context, h corehost.Host, store PeerStore, serviceName string) (*LocalService, error) {
	return StartLocalWithOptions(ctx, h, store, serviceName)
}

func StartLocalWithOptions(ctx context.Context, h corehost.Host, store PeerStore, serviceName string, opts ...LocalOption) (*LocalService, error) {
	options := localOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	notifee := &localNotifee{ctx: ctx, host: h, store: store, observer: options.observer}
	mdnsService := mdns.NewMdnsService(h, serviceName, notifee)
	if err := mdnsService.Start(); err != nil {
		return nil, fmt.Errorf("start mdns discovery: %w", err)
	}
	fileCtx, cancel := context.WithCancel(ctx)
	service := &LocalService{service: mdnsService, cancel: cancel}
	service.startFileRegistry(fileCtx, h, serviceName, notifee)
	go func() {
		<-ctx.Done()
		_ = service.Close()
	}()
	return service, nil
}

func (s *LocalService) Close() error {
	if s == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.service != nil {
		if err := s.service.Close(); err != nil {
			s.wg.Wait()
			return err
		}
	}
	s.wg.Wait()
	return nil
}

type filePeerRecord struct {
	PeerID    string    `json:"peer_id"`
	Addrs     []string  `json:"addrs"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *LocalService) startFileRegistry(ctx context.Context, h corehost.Host, serviceName string, notifee *localNotifee) {
	dir, err := localRegistryDir(serviceName)
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		seen := map[string]time.Time{}
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		publishAndScanLocalRegistry(ctx, dir, h, notifee, seen)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				publishAndScanLocalRegistry(ctx, dir, h, notifee, seen)
			}
		}
	}()
}

func publishAndScanLocalRegistry(ctx context.Context, dir string, h corehost.Host, notifee *localNotifee, seen map[string]time.Time) {
	if ctx.Err() != nil {
		return
	}
	_ = writeLocalRegistryRecord(dir, h)
	records, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range records {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		record, err := readLocalRegistryRecord(filepath.Join(dir, entry.Name()))
		if err != nil || record.PeerID == h.ID().String() || time.Since(record.UpdatedAt) > time.Minute {
			continue
		}
		if last, ok := seen[record.PeerID]; ok && time.Since(last) < 30*time.Second {
			continue
		}
		addrs := parseMultiaddrs(record.Addrs)
		if len(addrs) == 0 {
			continue
		}
		infos, err := peer.AddrInfosFromP2pAddrs(addrs...)
		if err != nil || len(infos) == 0 || infos[0].ID == h.ID() {
			continue
		}
		seen[record.PeerID] = time.Now()
		notifee.handlePeerFound(infos[0], "local-registry")
	}
}

func writeLocalRegistryRecord(dir string, h corehost.Host) error {
	interfaceAddrs, err := h.Network().InterfaceListenAddresses()
	if err != nil {
		interfaceAddrs = h.Addrs()
	}
	addrs, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{ID: h.ID(), Addrs: interfaceAddrs})
	if err != nil {
		return err
	}
	raw := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		raw = append(raw, addr.String())
	}
	record := filePeerRecord{PeerID: h.ID().String(), Addrs: raw, UpdatedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, h.ID().String()+".json"), data, 0o600)
}

func readLocalRegistryRecord(path string) (filePeerRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return filePeerRecord{}, err
	}
	var record filePeerRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return filePeerRecord{}, err
	}
	return record, nil
}

func parseMultiaddrs(raw []string) []multiaddr.Multiaddr {
	addrs := make([]multiaddr.Multiaddr, 0, len(raw))
	for _, value := range raw {
		addr, err := multiaddr.NewMultiaddr(value)
		if err == nil {
			addrs = append(addrs, addr)
		}
	}
	return addrs
}

func localRegistryDir(serviceName string) (string, error) {
	if dir := os.Getenv("P2PBK_LOCAL_DISCOVERY_DIR"); dir != "" {
		return dir, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "p2pbk", "discovery", sanitizeRegistryName(serviceName)), nil
}

func sanitizeRegistryName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = mdns.ServiceName
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return replacer.Replace(value)
}

func ConnectConfiguredAndSaved(ctx context.Context, h corehost.Host, configured []string, store PeerStore, opts ...ConnectOption) ([]peer.AddrInfo, error) {
	options := connectOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	addrs := append([]string(nil), configured...)
	if store != nil {
		records, err := store.ListPeers()
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			addrs = append(addrs, record.Addrs...)
		}
	}
	addrs = uniqueStrings(addrs)

	connected := make([]peer.AddrInfo, 0, len(addrs))
	for _, addr := range addrs {
		info, err := p2phost.ParseAddrInfo(addr)
		if err != nil {
			continue
		}
		if info.ID == h.ID() || options.filter.Ignored(info.ID) {
			continue
		}
		if err := p2phost.Connect(ctx, h, info); err != nil {
			if store != nil {
				_ = store.RecordPeerFailure(info.ID.String(), time.Now().UTC())
			}
			continue
		}
		connected = append(connected, info)
		if store != nil {
			_ = SavePeerInfo(store, info, "connected")
		}
	}
	return connected, nil
}

func SaveConnectedPeers(store PeerStore, h corehost.Host, source string, opts ...ConnectOption) error {
	options := connectOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	if store == nil {
		return nil
	}
	for _, id := range h.Network().Peers() {
		if id == "" || id == h.ID() || options.filter.Ignored(id) {
			continue
		}
		info := peer.AddrInfo{ID: id, Addrs: h.Peerstore().Addrs(id)}
		if err := SavePeerInfo(store, info, source); err != nil {
			return err
		}
	}
	return nil
}

func SavePeerInfo(store PeerStore, info peer.AddrInfo, source string) error {
	if store == nil || info.ID == "" {
		return nil
	}
	addrs, err := peer.AddrInfoToP2pAddrs(&info)
	if err != nil {
		return err
	}
	raw := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		raw = append(raw, addr.String())
	}
	return store.UpsertPeer(localdb.PeerRecord{
		ID:       info.ID.String(),
		Addrs:    raw,
		Source:   source,
		LastSeen: time.Now().UTC(),
	})
}

type localNotifee struct {
	ctx      context.Context
	host     corehost.Host
	store    PeerStore
	observer func(Event)
}

func (n *localNotifee) HandlePeerFound(info peer.AddrInfo) {
	n.handlePeerFound(info, "mdns")
}

func (n *localNotifee) handlePeerFound(info peer.AddrInfo, source string) {
	if info.ID == "" || info.ID == n.host.ID() {
		return
	}
	n.emit(Event{Type: EventFound, PeerID: info.ID.String(), Addrs: addrStrings(info), Source: source})
	if err := p2phost.Connect(n.ctx, n.host, info); err != nil {
		if n.store != nil {
			_ = n.store.RecordPeerFailure(info.ID.String(), time.Now().UTC())
		}
		n.emit(Event{Type: EventConnectFailed, PeerID: info.ID.String(), Addrs: addrStrings(info), Source: source, Err: err})
		return
	}
	n.emit(Event{Type: EventConnected, PeerID: info.ID.String(), Addrs: addrStrings(info), Source: source})
	if err := SavePeerInfo(n.store, info, source); err != nil {
		n.emit(Event{Type: EventSaveFailed, PeerID: info.ID.String(), Addrs: addrStrings(info), Source: source, Err: err})
		return
	}
	n.emit(Event{Type: EventSaved, PeerID: info.ID.String(), Addrs: addrStrings(info), Source: source})
}

func (n *localNotifee) emit(event Event) {
	if n.observer != nil {
		n.observer(event)
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func addrStrings(info peer.AddrInfo) []string {
	addrs, err := peer.AddrInfoToP2pAddrs(&info)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.String())
	}
	return out
}
