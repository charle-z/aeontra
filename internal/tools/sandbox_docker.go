package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// DockerSandboxConfig configures the Docker-based L3 backend. It runs a command in
// an ephemeral, hardened container: no network by default (egress default-deny),
// read-only rootfs, all capabilities dropped, no-new-privileges, non-root, with
// resource limits and only the workspace bind-mounted. This backend must run on a
// host that has Docker AND is NOT the socketless public MCP container (see
// docs/l3-sandbox-plan.md): mounting the Docker socket into the internet-facing
// daemon is forbidden.
type DockerSandboxConfig struct {
	Image     string // container image the command runs in (e.g. "golang:1.26-alpine")
	Root      string // jailed workspace root, bind-mounted rw
	Network   string // "none" (default, egress deny) — allowlist networking is future work
	User      string // uid:gid, non-root (default "10001:10001")
	MemoryMB  int    // memory limit (default 512)
	CPUs      string // cpu limit (default "1")
	PidsLimit int    // process limit (default 256)
	TmpfsSize string // /tmp tmpfs size (default "64m")
	Timeout   time.Duration
	exec      dockerExecFunc // injectable for tests; nil = real docker exec
}

// dockerExecFunc runs `docker <args>` and returns stdout, stderr, exit code, error.
type dockerExecFunc func(ctx context.Context, args []string) (stdout, stderr string, exitCode int, err error)

// NewDockerSandboxRunner builds the Docker backend with secure defaults filled in.
func NewDockerSandboxRunner(cfg DockerSandboxConfig) SandboxRunner {
	if cfg.Network == "" {
		cfg.Network = "none"
	}
	if cfg.User == "" {
		cfg.User = "10001:10001"
	}
	if cfg.MemoryMB == 0 {
		cfg.MemoryMB = 512
	}
	if cfg.CPUs == "" {
		cfg.CPUs = "1"
	}
	if cfg.PidsLimit == 0 {
		cfg.PidsLimit = 256
	}
	if cfg.TmpfsSize == "" {
		cfg.TmpfsSize = "64m"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Minute
	}
	if cfg.exec == nil {
		cfg.exec = realDockerExec
	}
	return dockerSandboxRunner{cfg: cfg}
}

type dockerSandboxRunner struct{ cfg DockerSandboxConfig }

func (d dockerSandboxRunner) Status(context.Context) SandboxStatusInfo {
	return SandboxStatusInfo{
		Available:     true,
		Backend:       "docker",
		DefaultEgress: d.cfg.Network, // "none" = deny
		FreeTerminal:  false,
		Notes: []string{
			"docker sandbox: --network " + d.cfg.Network + ", read-only rootfs, cap-drop ALL, no-new-privileges, non-root",
			"only the workspace is mounted; no Docker socket, no host mounts",
			"CONTAINMENT MUST BE ADVERSARIALLY VERIFIED on Linux (escape/egress/timeout) before enabling in prod",
		},
	}
}

func (d dockerSandboxRunner) Run(ctx context.Context, req SandboxRunRequest) (SandboxRunResult, error) {
	if len(req.Argv) == 0 {
		return SandboxRunResult{}, fmt.Errorf("sandbox: empty argv")
	}
	ctx, cancel := context.WithTimeout(ctx, d.cfg.Timeout)
	defer cancel()
	args := dockerArgv(d.cfg, req)
	start := time.Now()
	stdout, stderr, code, err := d.cfg.exec(ctx, args)
	return SandboxRunResult{
		ExitCode:       code,
		Stdout:         stdout,
		Stderr:         stderr,
		Duration:       time.Since(start),
		SandboxBackend: "docker",
		EgressProfile:  d.cfg.Network,
	}, err
}

// dockerArgv builds the hardened `docker run` argument list (excluding the leading
// "docker"). This is the security-critical surface; it is unit-tested to lock the
// isolation flags. It NEVER uses a shell and NEVER mounts the Docker socket.
func dockerArgv(cfg DockerSandboxConfig, req SandboxRunRequest) []string {
	net := cfg.Network
	if net == "" {
		net = "none"
	}
	args := []string{
		"run", "--rm",
		"--network", net,
		"--read-only",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=" + cfg.TmpfsSize,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", fmt.Sprintf("%d", cfg.PidsLimit),
		"--memory", fmt.Sprintf("%dm", cfg.MemoryMB),
		"--cpus", cfg.CPUs,
		"--user", cfg.User,
		// Bind only the workspace (rw); rootfs stays read-only.
		"--mount", "type=bind,src=" + cfg.Root + ",dst=" + cfg.Root,
		"--workdir", req.Dir,
	}
	// Allowlisted env only, deterministically ordered.
	keys := make([]string, 0, len(req.EnvAllowlist))
	for k := range req.EnvAllowlist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+req.EnvAllowlist[k])
	}
	args = append(args, cfg.Image)
	args = append(args, req.Argv...)
	return args
}

func realDockerExec(ctx context.Context, args []string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	// Non-zero exit is a normal command result, not a Go error we must surface as failure.
	if _, ok := err.(*exec.ExitError); ok {
		err = nil
	}
	return strings.TrimRight(outBuf.String(), "\n"), strings.TrimRight(errBuf.String(), "\n"), code, err
}
