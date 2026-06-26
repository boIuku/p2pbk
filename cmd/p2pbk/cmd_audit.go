package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"p2pbk/internal/app"
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

			result, err := app.RunAudit(ctx, app.AuditRequest{Config: currentConfig(), Limit: limit, Timeout: timeout})
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result.Output)
			return nil
		},
	}
	now.Flags().IntVar(&limit, "limit", 25, "maximum receipts to audit in one run; use 0 for all")
	now.Flags().DurationVar(&timeout, "timeout", 2*time.Second, "timeout per connect and shard check")
	cmd.AddCommand(now)
	return cmd
}
