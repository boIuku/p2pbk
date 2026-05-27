package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
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
