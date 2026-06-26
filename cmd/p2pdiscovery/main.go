package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	p2pdht "p2pbk/internal/network/dht"
	p2phost "p2pbk/internal/network/host"
)

const (
	defaultListenAddr = "/ip4/0.0.0.0/tcp/4001"
	envListenAddrs    = "P2PDISCOVERY_LISTEN_ADDRS"
	envAnnounceAddrs  = "P2PDISCOVERY_ANNOUNCE_ADDRS"
	envNATPortMap     = "P2PDISCOVERY_NAT_PORT_MAP"
	envRelayService   = "P2PDISCOVERY_RELAY_SERVICE"
)

type options struct {
	ListenAddrs        []string
	AnnounceAddrs      []string
	EnableNATPortMap   bool
	EnableRelayService bool
}

func main() {
	cmd := newRootCommand(run)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCommand(runFn func(context.Context, options, io.Writer) error) *cobra.Command {
	opts := optionsFromEnv()
	cmd := &cobra.Command{
		Use:   "p2pdiscovery",
		Short: "Run a public p2pbk libp2p discovery node",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runFn(ctx, opts, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringSliceVar(&opts.ListenAddrs, "listen-addr", opts.ListenAddrs, "libp2p listen multiaddr; repeat or comma-separate for multiple addresses")
	cmd.Flags().StringSliceVar(&opts.AnnounceAddrs, "announce-addr", opts.AnnounceAddrs, "public libp2p multiaddr to advertise; repeat or comma-separate for multiple addresses")
	cmd.Flags().BoolVar(&opts.EnableNATPortMap, "nat-port-map", opts.EnableNATPortMap, "try to open the listen port with UPnP/NAT-PMP")
	cmd.Flags().BoolVar(&opts.EnableRelayService, "relay-service", opts.EnableRelayService, "run libp2p circuit relay v2 service")
	return cmd
}

func optionsFromEnv() options {
	return options{
		ListenAddrs:        envStringSlice(envListenAddrs, []string{defaultListenAddr}),
		AnnounceAddrs:      envStringSlice(envAnnounceAddrs, nil),
		EnableNATPortMap:   envBool(envNATPortMap, true),
		EnableRelayService: envBool(envRelayService, true),
	}
}

func run(ctx context.Context, opts options, out io.Writer) error {
	if len(opts.ListenAddrs) == 0 {
		return errors.New("at least one listen address is required")
	}
	h, err := p2phost.New(ctx, p2phost.Options{
		ListenAddrs:        opts.ListenAddrs,
		AnnounceAddrs:      opts.AnnounceAddrs,
		EnableNATPortMap:   opts.EnableNATPortMap,
		EnableRelayService: opts.EnableRelayService,
	})
	if err != nil {
		return err
	}
	defer h.Close()

	dht, err := p2pdht.New(ctx, h, p2pdht.ModeServer)
	if err != nil {
		return err
	}
	defer dht.Close()
	if err := dht.Bootstrap(ctx); err != nil {
		return err
	}

	fmt.Fprintf(out, "p2pbk public discovery peer: %s\n", h.ID())
	fmt.Fprintln(out, "bootstrap addresses:")
	for _, addr := range h.Addrs() {
		fmt.Fprintf(out, "%s/p2p/%s\n", addr, h.ID())
	}
	fmt.Fprintf(out, "dht: /p2pbk server\n")
	fmt.Fprintf(out, "relay service: %t\n", opts.EnableRelayService)
	fmt.Fprintf(out, "nat port map: %t\n", opts.EnableNATPortMap)

	<-ctx.Done()
	return nil
}

func envStringSlice(name string, fallback []string) []string {
	raw := os.Getenv(name)
	if strings.TrimSpace(raw) == "" {
		return append([]string(nil), fallback...)
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), fallback...)
	}
	return out
}

func envBool(name string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}
