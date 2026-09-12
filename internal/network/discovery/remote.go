package discovery

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

const defaultRemoteDiscoveryLimit = 50

type RemoteRouter interface {
	Advertise(context.Context, string, time.Duration) (time.Duration, error)
	FindPeers(context.Context, string, int) (<-chan peer.AddrInfo, error)
}

type RemoteService struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type remoteOptions struct {
	observer     func(Event)
	interval     time.Duration
	roundTimeout time.Duration
	limit        int
	filter       PeerFilter
	requireRelay bool
}

type RemoteOption func(*remoteOptions)

func WithRemoteObserver(observer func(Event)) RemoteOption {
	return func(opts *remoteOptions) {
		opts.observer = observer
	}
}

func WithRemoteInterval(interval time.Duration) RemoteOption {
	return func(opts *remoteOptions) {
		opts.interval = interval
	}
}

func WithRemoteRoundTimeout(timeout time.Duration) RemoteOption {
	return func(opts *remoteOptions) {
		opts.roundTimeout = timeout
	}
}

func WithRemoteLimit(limit int) RemoteOption {
	return func(opts *remoteOptions) {
		opts.limit = limit
	}
}

func WithRemoteIgnoredPeers(filter PeerFilter) RemoteOption {
	return func(opts *remoteOptions) {
		opts.filter = filter
	}
}

func WithRemoteRequireRelayAddress(require bool) RemoteOption {
	return func(opts *remoteOptions) {
		opts.requireRelay = require
	}
}

func StartRemote(ctx context.Context, h corehost.Host, store PeerStore, router RemoteRouter, namespace string, opts ...RemoteOption) (*RemoteService, error) {
	if h == nil {
		return nil, fmt.Errorf("host is required")
	}
	if router == nil {
		return nil, fmt.Errorf("remote discovery router is required")
	}
	if namespace == "" {
		return nil, fmt.Errorf("remote discovery namespace is required")
	}

	options := remoteOptions{
		interval: time.Minute,
		limit:    defaultRemoteDiscoveryLimit,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if options.interval <= 0 {
		return nil, fmt.Errorf("remote discovery interval must be positive")
	}
	if options.roundTimeout <= 0 {
		options.roundTimeout = options.interval
		if options.roundTimeout > 30*time.Second {
			options.roundTimeout = 30 * time.Second
		}
	}
	if options.limit <= 0 {
		options.limit = defaultRemoteDiscoveryLimit
	}

	remoteCtx, cancel := context.WithCancel(ctx)
	service := &RemoteService{cancel: cancel}
	notifee := &localNotifee{ctx: remoteCtx, host: h, store: store, observer: options.observer}

	service.wg.Add(1)
	go func() {
		defer service.wg.Done()
		ticker := time.NewTicker(options.interval)
		defer ticker.Stop()
		runRemoteDiscoveryRound(remoteCtx, h, router, namespace, options.interval, options.roundTimeout, options.limit, notifee, options.filter, options.requireRelay)
		for {
			select {
			case <-remoteCtx.Done():
				return
			case <-ticker.C:
				runRemoteDiscoveryRound(remoteCtx, h, router, namespace, options.interval, options.roundTimeout, options.limit, notifee, options.filter, options.requireRelay)
			}
		}
	}()
	go func() {
		<-ctx.Done()
		_ = service.Close()
	}()
	return service, nil
}

func (s *RemoteService) Close() error {
	if s == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	return nil
}

func runRemoteDiscoveryRound(ctx context.Context, h corehost.Host, router RemoteRouter, namespace string, interval, roundTimeout time.Duration, limit int, notifee *localNotifee, filter PeerFilter, requireRelay bool) {
	if ctx.Err() != nil {
		return
	}
	roundCtx := ctx
	cancel := func() {}
	if roundTimeout > 0 {
		roundCtx, cancel = context.WithTimeout(ctx, roundTimeout)
	}
	defer cancel()
	if readyForRemoteAdvertise(h.Addrs(), requireRelay) {
		_, _ = router.Advertise(roundCtx, namespace, interval*2)
	}
	peers, err := router.FindPeers(roundCtx, namespace, limit)
	if err != nil {
		notifee.emit(Event{Type: EventConnectFailed, Source: "remote-discovery", Err: err})
		return
	}
	for {
		var info peer.AddrInfo
		select {
		case <-roundCtx.Done():
			return
		case found, ok := <-peers:
			if !ok {
				return
			}
			info = found
		}
		if roundCtx.Err() != nil {
			return
		}
		if info.ID == h.ID() || filter.Ignored(info.ID) || !hasUsefulRemoteAddr(info.Addrs, requireRelay) {
			continue
		}
		notifee.handlePeerFound(info, "remote-discovery")
	}
}

func readyForRemoteAdvertise(addrs []multiaddr.Multiaddr, requireRelay bool) bool {
	return hasUsefulRemoteAddr(addrs, requireRelay)
}

func hasUsefulRemoteAddr(addrs []multiaddr.Multiaddr, requireRelay bool) bool {
	for _, addr := range addrs {
		if isRelayAddr(addr) {
			return true
		}
	}
	if requireRelay {
		return false
	}
	for _, addr := range addrs {
		if isPublicAddr(addr) {
			return true
		}
	}
	return false
}

func isRelayAddr(addr multiaddr.Multiaddr) bool {
	_, err := addr.ValueForProtocol(multiaddr.P_CIRCUIT)
	return err == nil
}

func isPublicAddr(addr multiaddr.Multiaddr) bool {
	if hasDNSAddr(addr) {
		return true
	}
	if value, err := addr.ValueForProtocol(multiaddr.P_IP4); err == nil {
		return isPublicIP(value)
	}
	if value, err := addr.ValueForProtocol(multiaddr.P_IP6); err == nil {
		return isPublicIP(value)
	}
	return false
}

func hasDNSAddr(addr multiaddr.Multiaddr) bool {
	for _, protocol := range []int{multiaddr.P_DNS, multiaddr.P_DNS4, multiaddr.P_DNS6, multiaddr.P_DNSADDR} {
		if _, err := addr.ValueForProtocol(protocol); err == nil {
			return true
		}
	}
	return false
}

func isPublicIP(value string) bool {
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}
