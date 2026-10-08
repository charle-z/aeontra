// Command mcp-catalog-id prints or verifies the deterministic exterior catalog identity.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/charle-z/mcp-devbox/internal/catalogidentity"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	info, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		fmt.Fprintln(stderr, "catalog identity unavailable")
		return 1
	}
	identity := catalogidentity.Identity{
		ProtocolVersion: info.ProtocolVersion,
		ToolCount:       info.ToolCount,
		CatalogHash:     info.CatalogHash,
	}
	switch {
	case len(args) == 0:
		fmt.Fprintln(stdout, identity.CatalogHash)
		return 0
	case len(args) == 1 && args[0] == "--json":
		data, err := catalogidentity.EncodeManifest(identity)
		if err != nil {
			fmt.Fprintln(stderr, "catalog identity unavailable")
			return 1
		}
		_, _ = stdout.Write(data)
		return 0
	case len(args) == 2 && args[0] == "--verify":
		data, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Fprintln(stderr, "catalog identity manifest could not be read")
			return 1
		}
		manifest, err := catalogidentity.DecodeManifest(data)
		if err != nil {
			fmt.Fprintf(stderr, "catalog identity manifest is invalid: %v\n", err)
			return 1
		}
		if err := manifest.Matches(identity); err != nil {
			fmt.Fprintf(stderr, "catalog identity manifest does not match: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "catalog identity verified: %s tools=%d protocol=%s\n", identity.CatalogHash, identity.ToolCount, identity.ProtocolVersion)
		return 0
	default:
		fmt.Fprintln(stderr, "usage: mcp-catalog-id [--json | --verify PATH]")
		return 2
	}
}
