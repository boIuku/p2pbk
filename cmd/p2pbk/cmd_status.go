package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"p2pbk/internal/config"
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
			cfg := config.FromViper(viper.GetViper())

			paths, err := cfg.Paths()
			if err != nil {
				return err
			}

			var storeDir int64
			if file, err := os.Open(paths.StoreDir); err == nil {
				stats, _ := file.Stat()
				storeDir = stats.Size()
				file.Close()
			}

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
