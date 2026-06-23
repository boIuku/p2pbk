package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"p2pbk/internal/audit"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/peer/localdb"
)

func newAuditCommand() *cobra.Command {
	var limit int
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit peers that store this node's shards",
	}
	now := &cobra.Command{
		Use:   "now",
		Short: "Run one peer storage audit",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			cfg := currentConfig()
			paths, err := cfg.Paths()
			if err != nil {
				return err
			}
			db, err := localdb.Open(paths.LocalDB)
			if err != nil {
				return err
			}
			defer db.Close()

			host, err := p2phost.New(ctx, p2phost.Options{ListenAddrs: cfg.Network.ListenAddrs})
			if err != nil {
				return err
			}
			defer host.Close()

			summary, err := audit.RunOnce(ctx, audit.Options{
				Config:         cfg,
				Host:           host,
				DB:             db,
				Output:         cmd.OutOrStdout(),
				Limit:          limit,
				ConnectTimeout: timeout,
				ShardTimeout:   timeout,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "audit summary: checked=%d ok=%d failed=%d\n", summary.Checked, summary.Succeeded, summary.Failed)
			return nil
		},
	}
	now.Flags().IntVar(&limit, "limit", 25, "maximum receipts to audit in one run; use 0 for all")
	now.Flags().DurationVar(&timeout, "timeout", 2*time.Second, "timeout per connect and shard check")
	cmd.AddCommand(now)
	return cmd
}
