package app

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/console"
	"github.com/charle-z/mcp-devbox/internal/observability"
)

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

type serveOptions struct {
	Config          config.Config
	AuditPath       string
	HTTPAddr        string
	HTTPToken       string
	BrainRoot       string
	TaskRoot        string
	StateRoot       string
	ConsoleTimezone string
	Observability   observability.Config
}

func resolveBrainRoot(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.ContainsRune(raw, '\x00') || !filepath.IsAbs(raw) {
		return "", fmt.Errorf("%s must be an absolute path", brainRootEnv)
	}
	return filepath.Clean(raw), nil
}

func resolveTaskRoot(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.ContainsRune(raw, '\x00') || !filepath.IsAbs(raw) {
		return "", fmt.Errorf("%s must be an absolute path", taskRootEnv)
	}
	return filepath.Clean(raw), nil
}

func resolveStateRoot(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.ContainsRune(raw, '\x00') || !filepath.IsAbs(raw) {
		return "", fmt.Errorf("%s must be an absolute path", stateRootEnv)
	}
	return filepath.Clean(raw), nil
}

func parseServeOptions(args []string, output io.Writer) (serveOptions, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(output)
	var roots rootsFlag
	fs.Var(&roots, "root", "project root (absolute); repeatable or comma-separated")
	mode := fs.String("mode", string(config.ModeReadOnly), "read-only|ask|allow")
	allowCmd := fs.String("allow-cmd", "", "comma-separated command allowlist")
	testCmd := fs.String("test-cmd", "", `run_tests command, e.g. "go test ./..."`)
	auditPath := fs.String("audit", "", "audit log path")
	httpAddr := fs.String("http", "", "serve MCP over HTTP at ADDR (e.g. :8765); omit for stdio")
	httpToken := fs.String("http-token", "", "bearer token for HTTP (prefer "+tokenEnv+" env)")
	sandbox := fs.String("sandbox", "", "L3 sandbox backend: none (default)|private-rootless; legacy names remain unavailable")
	observabilityMode := fs.String("observability", "", "structured events: off|stderr|file|both (default: stderr)")
	observabilityPath := fs.String("observability-path", "", "absolute private JSONL path for file/both mode")
	observabilityMaxBytes := fs.String("observability-max-bytes", "", "rotation limit in bytes (default: 16777216)")
	if err := fs.Parse(args); err != nil {
		return serveOptions{}, err
	}
	if len(roots) == 0 {
		return serveOptions{}, fmt.Errorf("at least one --root is required")
	}

	absRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			return serveOptions{}, fmt.Errorf("resolving root %q: %w", root, err)
		}
		absRoots = append(absRoots, abs)
	}

	resolvedAllowCmd := envFallback(*allowCmd, allowCmdEnv)
	resolvedTestCmd := envFallback(*testCmd, testCmdEnv)
	resolvedSandbox := envFallback(*sandbox, sandboxEnv)

	allow := config.SecureDefaults().AllowedCommands
	if strings.TrimSpace(resolvedAllowCmd) != "" {
		allow = splitCSV(resolvedAllowCmd)
	}
	test := splitFields(resolvedTestCmd)
	if len(test) > 0 {
		allow = appendUnique(allow, test[0])
	}

	cfg, err := config.New(config.Config{
		Roots:           absRoots,
		Mode:            config.Mode(*mode),
		AllowedCommands: allow,
		TestCommand:     test,
		SandboxBackend:  resolvedSandbox,
	})
	if err != nil {
		return serveOptions{}, err
	}
	observabilityConfig, err := parseObservabilityConfig(*observabilityMode, *observabilityPath, *observabilityMaxBytes)
	if err != nil {
		return serveOptions{}, err
	}
	brainRoot, err := resolveBrainRoot(os.Getenv(brainRootEnv))
	if err != nil {
		return serveOptions{}, err
	}
	taskRoot, err := resolveTaskRoot(os.Getenv(taskRootEnv))
	if err != nil {
		return serveOptions{}, err
	}
	stateRoot, err := resolveStateRoot(os.Getenv(stateRootEnv))
	if err != nil {
		return serveOptions{}, err
	}
	consoleTimezone, err := console.ValidateTimezone(os.Getenv(consoleTimezoneEnv))
	if err != nil {
		return serveOptions{}, fmt.Errorf("%s: %w", consoleTimezoneEnv, err)
	}
	return serveOptions{
		Config:          cfg,
		AuditPath:       *auditPath,
		HTTPAddr:        *httpAddr,
		HTTPToken:       *httpToken,
		BrainRoot:       brainRoot,
		TaskRoot:        taskRoot,
		StateRoot:       stateRoot,
		ConsoleTimezone: consoleTimezone,
		Observability:   observabilityConfig,
	}, nil
}
