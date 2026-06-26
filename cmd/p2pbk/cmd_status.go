package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"p2pbk/internal/app"
)

type statusOptions struct {
	long bool
}

func newStatusCommand() *cobra.Command {
	opts := statusOptions{}

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show peer status and storage usage",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := currentConfig()
			status, err := app.CollectStatus(context.Background(), cfg, configFileUsed(), "")
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "p2pbk Status")
			fmt.Fprintln(out, "============")
			fmt.Fprintf(out, "Storage: %d bytes\n", status.StorageBytes)
			fmt.Fprintf(out, "Storage mode: %s\n", status.StorageMode)
			fmt.Fprintf(out, "Data dir: %s\n", status.DataDir)
			fmt.Fprintf(out, "Store dir: %s\n", status.StoreDir)
			fmt.Fprintf(out, "Cache dir: %s\n", status.CacheDir)
			if status.LocalDBStatus == "" || status.LocalDBStatus == "available" {
				fmt.Fprintf(out, "LocalDB: %s\n", status.LocalDB)
			} else {
				fmt.Fprintf(out, "LocalDB: %s (%s)\n", status.LocalDB, status.LocalDBStatus)
			}
			fmt.Fprintf(out, "Protected roots: %d\n", len(status.Roots))
			for _, root := range status.Roots {
				fmt.Fprintf(out, "  - %s (%s, %s, snapshots=%d)\n", root.Path, root.Kind, root.Protection, root.Snapshots)
			}
			fmt.Fprintf(out, "Backup schedule: %s\n", status.Schedule)
			fmt.Fprintf(out, "Retention: %s\n", status.Retention)
			fmt.Fprintf(out, "Max protected data: %d bytes\n", status.MaxProtectedBytes)
			if status.Repository == "initialized" {
				fmt.Fprintf(out, "Repository: initialized (%s)\n", status.RepositoryPath)
			} else {
				fmt.Fprintln(out, "Repository: not initialized; run `p2pbk repo init` or `p2pbk backup now`")
			}
			for _, root := range status.Roots {
				if root.LastSuccess != "" {
					fmt.Fprintf(out, "Last scheduled backup for %s: %s\n", root.Path, root.LastSuccess)
				}
				if root.LastFailure != "" {
					fmt.Fprintf(out, "Last scheduled backup failure for %s: %s\n", root.Path, root.LastFailure)
				}
			}
			fmt.Fprintf(out, "Known peers: %d\n", len(status.Peers))
			if opts.long {
				for _, peer := range status.Peers {
					fmt.Fprintf(out, "  - %s source=%s status=%s score=%.2f failures=%d last_seen=%s\n", peer.ID, peer.Source, peer.Status, peer.Score, peer.Failures, peer.LastSeen.Format("2006-01-02T15:04:05Z07:00"))
					if len(peer.Addrs) > 0 {
						fmt.Fprintf(out, "    addrs: %s\n", strings.Join(peer.Addrs, ", "))
					}
				}
			}
			fmt.Fprintln(out)

			if opts.long {
				fmt.Fprintln(out, "Detailed Status (long mode):")
				fmt.Fprintln(out, "------------------------------")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&opts.long, "long", false, "show detailed status")
	return cmd
}
