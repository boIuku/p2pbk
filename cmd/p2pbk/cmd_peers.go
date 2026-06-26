package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"p2pbk/internal/peer/localdb"
)

func newPeersCommand() *cobra.Command {
	return &cobra.Command{
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
				if localdb.IsOpenTimeout(err) {
					return fmt.Errorf("localdb is locked by the running daemon; peer discovery is still handled by the daemon, or stop the daemon before listing peers directly: %w", err)
				}
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
}

func shortPeer(peerID string) string {
	if len(peerID) <= 40 {
		return peerID
	}
	return peerID[:37] + "..."
}
