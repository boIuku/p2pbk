package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"p2pbk/internal/app"
	"p2pbk/internal/peer/localdb"
)

func newPeersCommand() *cobra.Command {
	var failureDomain string
	var note string

	cmd := &cobra.Command{
		Use:   "peers",
		Short: "List known peers and local trust history",
		RunE: func(cmd *cobra.Command, args []string) error {
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

			stats, err := db.ListPeerStats()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Peer                                      Score  Status   Shards  Audits")
			for _, stat := range stats {
				audits := fmt.Sprintf("%d/%d", stat.AuditSuccesses, stat.AuditSuccesses+stat.AuditFailures)
				fmt.Fprintf(cmd.OutOrStdout(), "%-41s %.2f   %-8s %6d  %s\n", shortPeer(stat.ID), stat.Score, stat.Status, stat.StoredShards, audits)
			}
			return nil
		},
	}
	approve := &cobra.Command{
		Use:   "approve <peer-id>",
		Short: "Explicitly approve an authenticated peer to store new shards here",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ApprovePeer(currentConfig(), args[0], failureDomain, note); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "approved %s for new shard storage\n", args[0])
			return nil
		},
	}
	approve.Flags().StringVar(&failureDomain, "failure-domain", "", "physical storage failure domain")
	approve.Flags().StringVar(&note, "note", "", "operator note")
	revoke := &cobra.Command{
		Use:   "revoke <peer-id>",
		Short: "Revoke new storage admission without deleting existing owner data",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.RevokePeer(currentConfig(), args[0], note); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "revoked %s; existing shards remain readable/deletable\n", args[0])
			return nil
		},
	}
	revoke.Flags().StringVar(&note, "note", "", "operator note")
	list := &cobra.Command{
		Use:   "approved",
		Short: "List explicit storage admissions",
		RunE: func(cmd *cobra.Command, args []string) error {
			admissions, err := app.ListPeerAdmissions(currentConfig())
			if err != nil {
				return err
			}
			for _, admission := range admissions {
				when := admission.ApprovedAt
				if !admission.RevokedAt.IsZero() {
					when = admission.RevokedAt
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s at=%s %s\n", app.FormatPeerAdmission(admission), when.Format(time.RFC3339), admission.Note)
			}
			return nil
		},
	}
	cmd.AddCommand(approve, revoke, list)
	return cmd
}

func shortPeer(peerID string) string {
	if len(peerID) <= 40 {
		return peerID
	}
	return peerID[:37] + "..."
}
