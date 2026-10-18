// Command mcp-catalog-id prints the deterministic exterior catalog hash for release automation.
package main

import (
	"fmt"
	"os"

	"github.com/charle-z/mcp-devbox/internal/mcpserver"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "mcp-catalog-id accepts no arguments")
		os.Exit(2)
	}
	info, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		fmt.Fprintln(os.Stderr, "catalog identity unavailable")
		os.Exit(1)
	}
	fmt.Println(info.CatalogHash)
}
