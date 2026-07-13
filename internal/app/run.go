package app

import (
	"fmt"
	"os"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
)

func Main() {
	stampCommit()
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		if err := serve(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "mcp-devbox: "+err.Error())
			os.Exit(1)
		}
	case "grant":
		if err := grant(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "mcp-devbox: "+err.Error())
			os.Exit(1)
		}
	case "version", "--version", "-v":
		fmt.Println("mcp-devbox " + buildinfo.Version + " (commit " + buildinfo.Commit + ")")
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintln(os.Stderr, "unknown command: "+os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `mcp-devbox `+buildinfo.Version+` — secure-by-default local MCP server

Usage:
  mcp-devbox serve --root <ABS_PATH> [--mode read-only|ask|allow] [flags]
  mcp-devbox grant --admin http://127.0.0.1:<PORT> --admin-token <TOKEN> [--ttl 5m] [--raw --confirm-raw] <REQUEST_ID>
  mcp-devbox version

serve flags:
  --root        project root (absolute). Repeat or comma-separate for multiple.
  --mode        access posture: read-only (default), ask, allow
  --allow-cmd   comma-separated command allowlist (default: git,go,ls,cat)
  --test-cmd    command for run_tests, e.g. "go test ./..."
  --audit       audit log path (default: <root>/.agent-memory/audit.log)
  --http        serve MCP over HTTP at ADDR (e.g. :8765). Omit for stdio (default).
                A host-less ADDR binds to 127.0.0.1 (use a tunnel for remote access).
  --http-token  bearer token for the HTTP endpoint. Prefer the `+tokenEnv+` env var.

Transports:
  stdio (default)  JSON-RPC on stdin/stdout (local clients: Cursor, Claude Desktop).
  http (--http)    JSON-RPC over POST /mcp; AUTH REQUIRED. Pass the token as
                   "Authorization: Bearer <t>" OR "/mcp?key=<t>" (ChatGPT can't send a
                   header → use ?key= + "Sin autenticación"). See docs/connect-remote.md.

Diagnostics go to stderr; the bearer token is never printed.
`)
}
