package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
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
	observer func(Event)
	interval time.Duration
	limit    int
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

func WithRemoteLimit(limit int) RemoteOption {
	return func(opts *remoteOptions) {
		opts.limit = limit
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
		runRemoteDiscoveryRound(remoteCtx, router, namespace, options.interval, options.limit, notifee)
		for {
			select {
			case <-remoteCtx.Done():
				return
			case <-ticker.C:
				runRemoteDiscoveryRound(remoteCtx, router, namespace, options.interval, options.limit, notifee)
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

func runRemoteDiscoveryRound(ctx context.Context, router RemoteRouter, namespace string, interval time.Duration, limit int, notifee *localNotifee) {
	if ctx.Err() != nil {
		return
	}
	_, _ = router.Advertise(ctx, namespace, interval*2)
	peers, err := router.FindPeers(ctx, namespace, limit)
	if err != nil {
		notifee.emit(Event{Type: EventConnectFailed, Source: "remote-discovery", Err: err})
		return
	}
	for info := range peers {
		if ctx.Err() != nil {
			return
		}
		notifee.handlePeerFound(info, "remote-discovery")
	}
}
