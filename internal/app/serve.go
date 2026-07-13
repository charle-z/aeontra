package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/grantadmin"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

func serve(args []string) error {
	opts, err := parseServeOptions(args, os.Stderr)
	if err != nil {
		return err
	}
	cfg := opts.Config
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		return err
	}
	primary := pol.Roots()[0]

	ap := opts.AuditPath
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
		WithTestCommand(cfg.TestCommand).
		WithSandboxRunner(sandboxRunner).
		WithValidationRunner(tools.NewValidationRunner(os.Getenv(validationRunnerURLEnv), os.Getenv(validationRunnerTokenEnv)))
	privilegedTimeout := 2 * time.Minute
	if raw := strings.TrimSpace(os.Getenv(privilegedTimeoutEnv)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("%s must be a positive duration", privilegedTimeoutEnv)
		}
		privilegedTimeout = parsed
	}
	svc = svc.WithPrivilegedConfig(tools.PrivilegedConfig{
		Enabled:         strings.EqualFold(strings.TrimSpace(os.Getenv(privilegedTasksEnv)), "true"),
		AllowedServices: splitCSV(os.Getenv(privilegedServicesEnv)),
		Timeout:         privilegedTimeout,
	})
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
			).
			WithGitHubApp(os.Getenv(coolifyGitHubAppUUIDEnv)).
			WithBuilderRuntime(os.Getenv(coolifyDestinationUUIDEnv), splitSemicolon(os.Getenv(coolifyAllowedMountsEnv))))
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
	if strings.TrimSpace(opts.HTTPAddr) != "" {
		token := opts.HTTPToken
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
		addr := normalizeHTTPAddr(opts.HTTPAddr)
		// Docker and Coolify stop containers with SIGTERM. Listening only for
		// os.Interrupt would prevent graceful draining during rolling replacement.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		authDesc := "Authorization: Bearer <token>"
		if oauthProvider != nil {
			authDesc = "OAuth (discovery via /.well-known/oauth-protected-resource)"
			if strings.TrimSpace(token) != "" {
				authDesc += " or legacy Bearer token"
			}
		}
		fmt.Fprintf(os.Stderr, "mcp-devbox %s serving HTTP on %s (mode=%s, roots=%v, audit=%s)\n",
			buildinfo.Version, addr, pol.Mode(), pol.Roots(), ap)
		fmt.Fprintf(os.Stderr, "MCP endpoint: POST http://%s%s  (%s)\n",
			addr, mcpserver.DefaultMCPPath, authDesc)
		return srv.ServeHTTP(ctx, addr, token, oauthProvider)
	}

	fmt.Fprintf(os.Stderr, "mcp-devbox %s serving stdio (mode=%s, roots=%v, audit=%s)\n",
		buildinfo.Version, pol.Mode(), pol.Roots(), ap)
	return srv.Serve(os.Stdin, os.Stdout)
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

func randomHexToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
