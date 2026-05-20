//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: bootstrap <config.yaml>")
		os.Exit(1)
	}

	fmt.Println("bootstrap command (placeholder)")
}
