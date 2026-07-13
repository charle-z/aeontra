package app

import (
	"fmt"
	"io"
	"os"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
)

func Main() {
	stampCommit()
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "serve":
		if err := serve(args[1:]); err != nil {
			fmt.Fprintln(stderr, "mcp-devbox: "+err.Error())
			return 1
		}
		return 0
	case "grant":
		if err := grant(args[1:]); err != nil {
			fmt.Fprintln(stderr, "mcp-devbox: "+err.Error())
			return 1
		}
		return 0
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "mcp-devbox "+buildinfo.Version+" (commit "+buildinfo.Commit+")")
		return 0
	case "help", "--help", "-h":
		usage(stderr)
		return 0
	default:
		fmt.Fprintln(stderr, "unknown command: "+args[0])
		usage(stderr)
		return 2
	}
}

func usage(output io.Writer) {
	fmt.Fprint(output, `mcp-devbox `+buildinfo.Version+` — secure-by-default local MCP server

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
