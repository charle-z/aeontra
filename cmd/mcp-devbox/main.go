// Command mcp-devbox is a secure-by-default local MCP server for AI coding agents.
// L1: read/search/patch/test a local repo safely over the MCP stdio transport.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/grantadmin"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
	"github.com/charle-z/mcp-devbox/internal/oauth"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

const version = "0.2.0"

// tokenEnv is the preferred way to supply the HTTP bearer token (keeps it out of
// the process argument list / shell history).
const tokenEnv = "MCP_DEVBOX_TOKEN"

// Env fallbacks for the test/allowlist commands so a containerized deploy (Coolify)
// can configure them without baking flags into the image. A flag, when set, wins.
const (
	testCmdEnv  = "MCP_DEVBOX_TEST_CMD"
	allowCmdEnv = "MCP_DEVBOX_ALLOW_CMD"
	sandboxEnv  = "MCP_DEVBOX_SANDBOX"
)

// OAuth env. When both are set, the HTTP transport enables its in-process OAuth
// authorization server (see internal/oauth). publicURLEnv is the public HTTPS base URL
// (the OAuth issuer); the canonical MCP resource used as the token audience is that base
// plus the MCP path.
const (
	publicURLEnv             = "MCP_DEVBOX_PUBLIC_URL"
	oauthPassphraseEnv       = "MCP_DEVBOX_OAUTH_PASSPHRASE"
	oauthClientStorePathEnv  = "MCP_DEVBOX_OAUTH_CLIENT_STORE"
	oauthRefreshStorePathEnv = "MCP_DEVBOX_OAUTH_REFRESH_STORE"
)

const (
	githubTokenEnv             = "GITHUB_TOKEN"
	githubOwnerEnv             = "GITHUB_OWNER"
	githubOwnerTypeEnv         = "GITHUB_OWNER_TYPE"
	githubDefaultVisibilityEnv = "GITHUB_DEFAULT_VISIBILITY"
)

const (
	coolifyURLEnv             = "COOLIFY_URL"
	coolifyAPITokenEnv        = "COOLIFY_API_TOKEN"
	coolifyAllowedAppsEnv     = "COOLIFY_ALLOWED_APPS"
	coolifyServerUUIDEnv      = "COOLIFY_SERVER_UUID"
	coolifyProjectUUIDEnv     = "COOLIFY_PROJECT_UUID"
	coolifyEnvironmentNameEnv = "COOLIFY_ENVIRONMENT_NAME"
	coolifyEnvironmentUUIDEnv = "COOLIFY_ENVIRONMENT_UUID"
	coolifyAllowedDomainsEnv  = "COOLIFY_ALLOWED_DOMAINS"
)

// buildOAuthProvider constructs the OAuth provider from env, or returns (nil, nil) when
// OAuth is not configured. It errors if only one of the two required vars is set, so a
// half-configured OAuth setup fails loudly rather than silently falling back.
func buildOAuthProvider() (*oauth.Provider, error) {
	publicURL := strings.TrimSpace(os.Getenv(publicURLEnv))
	passphrase := os.Getenv(oauthPassphraseEnv)
	if publicURL == "" && strings.TrimSpace(passphrase) == "" {
		return nil, nil // OAuth disabled
	}
	if publicURL == "" || strings.TrimSpace(passphrase) == "" {
		return nil, fmt.Errorf("OAuth requires BOTH %s and %s to be set", publicURLEnv, oauthPassphraseEnv)
	}
	issuer := strings.TrimRight(publicURL, "/")
	p, err := oauth.NewProvider(oauth.Config{
		Issuer:           issuer,
		Resource:         issuer + mcpserver.DefaultMCPPath,
		Passphrase:       passphrase,
		ClientStorePath:  os.Getenv(oauthClientStorePathEnv),
		RefreshStorePath: os.Getenv(oauthRefreshStorePathEnv),
	})
	if err != nil {
		return nil, fmt.Errorf("configuring OAuth: %w", err)
	}
	return p, nil
}

// envFallback returns flagVal when non-empty (after trimming), otherwise the value
// of the named environment variable.
func envFallback(flagVal, envName string) string {
	if strings.TrimSpace(flagVal) != "" {
		return flagVal
	}
	return os.Getenv(envName)
}

const adminTokenEnv = "MCP_DEVBOX_ADMIN_TOKEN"

func main() {
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
		fmt.Println("mcp-devbox " + version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintln(os.Stderr, "unknown command: "+os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `mcp-devbox `+version+` — secure-by-default local MCP server

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

// rootsFlag collects --root (repeatable and/or comma-separated).
type rootsFlag []string

func (r *rootsFlag) String() string { return strings.Join(*r, ",") }
func (r *rootsFlag) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*r = append(*r, p)
		}
	}
	return nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var roots rootsFlag
	fs.Var(&roots, "root", "project root (absolute); repeatable or comma-separated")
	mode := fs.String("mode", string(config.ModeReadOnly), "read-only|ask|allow")
	allowCmd := fs.String("allow-cmd", "", "comma-separated command allowlist")
	testCmd := fs.String("test-cmd", "", `run_tests command, e.g. "go test ./..."`)
	auditPath := fs.String("audit", "", "audit log path")
	httpAddr := fs.String("http", "", "serve MCP over HTTP at ADDR (e.g. :8765); omit for stdio")
	httpToken := fs.String("http-token", "", "bearer token for HTTP (prefer "+tokenEnv+" env)")
	sandbox := fs.String("sandbox", "", "L3 sandbox backend: none (default)|docker|nsjail|gvisor (plumbed, not yet enabled)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(roots) == 0 {
		return fmt.Errorf("at least one --root is required")
	}

	// Resolve roots to absolute paths (the policy requires absolute roots).
	absRoots := make([]string, 0, len(roots))
	for _, r := range roots {
		abs, err := filepath.Abs(r)
		if err != nil {
			return fmt.Errorf("resolving root %q: %w", r, err)
		}
		absRoots = append(absRoots, abs)
	}

	// Flags win; otherwise fall back to env (handy for container deploys).
	*allowCmd = envFallback(*allowCmd, allowCmdEnv)
	*testCmd = envFallback(*testCmd, testCmdEnv)
	*sandbox = envFallback(*sandbox, sandboxEnv)

	allow := config.SecureDefaults().AllowedCommands
	if strings.TrimSpace(*allowCmd) != "" {
		allow = splitCSV(*allowCmd)
	}
	test := splitFields(*testCmd)
	if len(test) > 0 {
		allow = appendUnique(allow, test[0]) // ensure the test program is allowlisted
	}

	cfg, err := config.New(config.Config{
		Roots:           absRoots,
		Mode:            config.Mode(*mode),
		AllowedCommands: allow,
		TestCommand:     test,
		SandboxBackend:  *sandbox,
	})
	if err != nil {
		return err
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		return err
	}
	primary := pol.Roots()[0]

	ap := *auditPath
	if ap == "" {
		ap = filepath.Join(primary, ".agent-memory", "audit.log")
	}
	if err := os.MkdirAll(filepath.Dir(ap), 0o755); err != nil {
		return fmt.Errorf("creating audit dir: %w", err)
	}
	logger, err := audit.Open(ap)
	if err != nil {
		return fmt.Errorf("opening audit log: %w", err)
	}
	defer logger.Close()

	// Select the L3 sandbox runner. "docker" gets the real hardened backend; other
	// named backends (nsjail/gvisor) stay pending until implemented; "none" disabled.
	// NOTE: the runner currently backs sandbox_status only — no tool routes command
	// execution through it yet (broad exec stays gated until adversarial Linux tests
	// pass; see docs/l3-sandbox-plan.md).
	sandboxRunner := tools.NewSandboxRunner(cfg.SandboxBackend)
	if cfg.SandboxBackend == "docker" {
		img := os.Getenv("MCP_DEVBOX_SANDBOX_IMAGE")
		if img == "" {
			img = "golang:1.26-alpine"
		}
		sandboxRunner = tools.NewDockerSandboxRunner(tools.DockerSandboxConfig{Image: img, Root: primary})
	}
	svc := tools.NewService(pol, logger, primary).
		WithTestCommand(test).
		WithSandboxRunner(sandboxRunner)
	// Optional Coolify deploy capability (disabled unless configured). The API token
	// is a secret read from env; it is never exposed to the agent.
	if cu := strings.TrimSpace(os.Getenv(coolifyURLEnv)); cu != "" {
		svc = svc.WithCoolify(tools.NewCoolifyClient(cu, os.Getenv(coolifyAPITokenEnv), splitCSV(os.Getenv(coolifyAllowedAppsEnv))).
			WithBuilderConfig(
				os.Getenv(coolifyServerUUIDEnv),
				os.Getenv(coolifyProjectUUIDEnv),
				os.Getenv(coolifyEnvironmentNameEnv),
				os.Getenv(coolifyEnvironmentUUIDEnv),
				splitCSV(os.Getenv(coolifyAllowedDomainsEnv)),
			))
	}
	if gt := strings.TrimSpace(os.Getenv(githubTokenEnv)); gt != "" {
		svc = svc.WithGitHub(tools.NewGitHubClient("", gt, os.Getenv(githubOwnerEnv), os.Getenv(githubOwnerTypeEnv), os.Getenv(githubDefaultVisibilityEnv)))
	}
	srv := mcpserver.New(svc)
	adminToken, err := randomHexToken()
	if err != nil {
		return fmt.Errorf("creating admin token: %w", err)
	}
	adminAddr, adminShutdown, err := grantadmin.Start("127.0.0.1:0", adminToken, pol, logger)
	if err != nil {
		return fmt.Errorf("starting local grant admin channel: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = adminShutdown(ctx)
	}()
	adminBase := "http://" + adminAddr
	pol.AccessGrants().SetNotifier(func(req policy.AccessRequest) {
		rawFlag := ""
		if req.RawRequested {
			rawFlag = " --raw --confirm-raw"
		}
		fmt.Fprintf(os.Stderr, "\nACCESS REQUIRED request_id=%s raw_requested=%t path=%s\n",
			req.ID, req.RawRequested, req.Path)
		fmt.Fprintf(os.Stderr, "Approve locally: mcp-devbox grant --admin %s --admin-token %s --ttl 5m%s %s\n\n",
			adminBase, adminToken, rawFlag, req.ID)
	})
	fmt.Fprintf(os.Stderr, "Local grant admin channel: %s (loopback only; token printed for local human approval)\n",
		adminBase)

	// HTTP transport (remote) when --http is set; stdio (local) otherwise.
	if strings.TrimSpace(*httpAddr) != "" {
		token := *httpToken
		if token == "" {
			token = os.Getenv(tokenEnv)
		}
		// Optional OAuth authorization server (enabled when both env vars are set). The
		// resource (token audience) is the canonical MCP URL = public base + /mcp.
		oauthProvider, err := buildOAuthProvider()
		if err != nil {
			return err
		}
		// Fail closed: at least one auth mechanism must be configured.
		if strings.TrimSpace(token) == "" && oauthProvider == nil {
			return fmt.Errorf("HTTP transport requires auth: set %s=<token> (or --http-token), "+
				"or enable OAuth with %s + %s. Refusing to start without auth",
				tokenEnv, publicURLEnv, oauthPassphraseEnv)
		}
		addr := normalizeHTTPAddr(*httpAddr)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		authDesc := "Authorization: Bearer <token>"
		if oauthProvider != nil {
			authDesc = "OAuth (discovery via /.well-known/oauth-protected-resource)"
			if strings.TrimSpace(token) != "" {
				authDesc += " or legacy Bearer token"
			}
		}
		fmt.Fprintf(os.Stderr, "mcp-devbox %s serving HTTP on %s (mode=%s, roots=%v, audit=%s)\n",
			version, addr, pol.Mode(), pol.Roots(), ap)
		fmt.Fprintf(os.Stderr, "MCP endpoint: POST http://%s%s  (%s)\n",
			addr, mcpserver.DefaultMCPPath, authDesc)
		return srv.ServeHTTP(ctx, addr, token, oauthProvider)
	}

	fmt.Fprintf(os.Stderr, "mcp-devbox %s serving stdio (mode=%s, roots=%v, audit=%s)\n",
		version, pol.Mode(), pol.Roots(), ap)
	return srv.Serve(os.Stdin, os.Stdout)
}

func grant(args []string) error {
	fs := flag.NewFlagSet("grant", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	admin := fs.String("admin", "", "local grant admin base URL printed by the daemon")
	adminToken := fs.String("admin-token", "", "local admin token printed by the daemon (or "+adminTokenEnv+")")
	ttl := fs.String("ttl", "5m", "short grant ttl, e.g. 5m")
	raw := fs.Bool("raw", false, "approve unredacted secret output")
	confirmRaw := fs.Bool("confirm-raw", false, "required together with --raw")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("grant requires exactly one REQUEST_ID")
	}
	if strings.TrimSpace(*admin) == "" {
		return fmt.Errorf("--admin is required")
	}
	token := *adminToken
	if token == "" {
		token = os.Getenv(adminTokenEnv)
	}
	if token == "" {
		return fmt.Errorf("--admin-token is required (or set %s)", adminTokenEnv)
	}
	if *raw && !*confirmRaw {
		return fmt.Errorf("--raw requires --confirm-raw")
	}
	body, err := json.Marshal(map[string]any{"ttl": *ttl, "raw": *raw})
	if err != nil {
		return err
	}
	url := strings.TrimRight(*admin, "/") + grantadmin.DefaultPath + "/" + fs.Arg(0)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("grant rejected: %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	fmt.Fprintln(os.Stdout, strings.TrimSpace(string(respBody)))
	return nil
}

// normalizeHTTPAddr binds host-less addresses to loopback by default. A bare port
// ("8765") or ":8765" becomes "127.0.0.1:8765"; an explicit host is preserved.
// Keeping the listener on loopback means a Cloudflare Tunnel (which connects out
// from the same host) is the only path in — no direct LAN/Internet exposure.
func normalizeHTTPAddr(a string) string {
	a = strings.TrimSpace(a)
	if !strings.Contains(a, ":") {
		a = ":" + a
	}
	if strings.HasPrefix(a, ":") {
		a = "127.0.0.1" + a
	}
	return a
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitFields(s string) []string { return strings.Fields(strings.TrimSpace(s)) }

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func randomHexToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
