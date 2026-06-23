package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"p2pbk/internal/audit"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/peer/localdb"
)

func newAuditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit peers that store this node's shards",
	}
	cmd.AddCommand(&cobra.Command{
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

			summary, err := audit.RunOnce(ctx, audit.Options{Config: cfg, Host: host, DB: db, Output: cmd.OutOrStdout()})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "audit summary: checked=%d ok=%d failed=%d\n", summary.Checked, summary.Succeeded, summary.Failed)
			return nil
		},
	})
	return cmd
}
