//go:build windows

package main

import (
	"fmt"
	"os"
)

func main() { fmt.Fprintln(os.Stderr, "mcp-autopilot-worker requires Linux"); os.Exit(1) }
