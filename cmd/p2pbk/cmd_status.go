package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"p2pbk/internal/kopiaengine"
	"p2pbk/internal/peer/localdb"
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

			paths, err := cfg.Paths()
			if err != nil {
				return err
			}

			storeDir := dirSize(paths.StoreDir)

			fmt.Printf("p2pbk Status\n")
			fmt.Println("============")
			fmt.Printf("Storage: %d bytes\n", storeDir)
			fmt.Printf("Data dir: %s\n", paths.DataDir)
			fmt.Printf("Store dir: %s\n", paths.StoreDir)
			fmt.Printf("Cache dir: %s\n", paths.CacheDir)
			fmt.Printf("Identity: %s\n", paths.Identity)
			fmt.Printf("LocalDB: %s\n", paths.LocalDB)
			fmt.Printf("Protected roots: %d\n", len(cfg.Backup.Roots))
			for _, root := range cfg.Backup.Roots {
				status := "missing"
				if info, err := os.Stat(root); err == nil {
					if info.IsDir() {
						status = "directory"
					} else {
						status = "file"
					}
				}
				fmt.Printf("  - %s (%s)\n", root, status)
			}
			fmt.Printf("Backup schedule: %s\n", cfg.Backup.Schedule)
			fmt.Printf("Retention: %s\n", cfg.Backup.Retention)
			fmt.Printf("Max protected data: %d bytes\n", cfg.Backup.MaxBytes)
			engine := kopiaengine.New(configFileUsed(), cfg, "")
			if engine.RepositoryConnected() {
				fmt.Printf("Repository: initialized (%s)\n", engine.RepositoryConfigPath())
			} else {
				fmt.Printf("Repository: not initialized; run `p2pbk repo init` or `p2pbk backup now`\n")
			}
			if db, err := localdb.Open(paths.LocalDB); err == nil {
				if peers, err := db.ListPeers(); err == nil {
					fmt.Printf("Known peers: %d\n", len(peers))
				}
				_ = db.Close()
			}
			fmt.Println()

			if opts.long {
				fmt.Println("Detailed Status (long mode):")
				fmt.Println("------------------------------")
				// Show settings from localdb if available
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&opts.long, "long", false, "show detailed status")
	return cmd
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
