package sandboxexecutor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

type podmanCommand func(ctx context.Context, argv []string, stdout, stderr io.Writer) (int, error)

type podmanEngine struct {
	socket string
	binary string
	uid    int
	gid    int
	run    podmanCommand
}

func (p *podmanEngine) Attest(ctx context.Context, image, digest string) error {
	rootless, err := p.capture(ctx, []string{"info", "--format", "{{.Host.Security.Rootless}}"}, 4096)
	if err != nil || strings.TrimSpace(rootless) != "true" {
		return errors.New("Podman endpoint is not an attested rootless engine")
	}
	actualDigest, err := p.capture(ctx, []string{"image", "inspect", "--format", "{{.Digest}}", image}, 4096)
	if err != nil || strings.TrimSpace(actualDigest) != digest {
		return errors.New("sandbox image digest does not match the pinned identity")
	}
	return nil
}

func (p *podmanEngine) Run(ctx context.Context, spec RunSpec) (sandboxprotocol.Response, error) {
	if p.run == nil {
		return sandboxprotocol.Response{}, errors.New("Podman command runner is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()
	streams := newBoundedStreams(spec.OutputBytes)
	start := time.Now()
	code, runErr := p.run(ctx, podmanRunArgv(spec, p.uid, p.gid), streams.stdout(), streams.stderr())
	duration := time.Since(start)
	cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cleanupCancel()
	_, cleanupErr := p.run(cleanupContext, []string{"rm", "--force", podmanContainerName(spec.IdempotencyKey)}, io.Discard, io.Discard)
	if runErr != nil {
		if ctx.Err() != nil {
			return sandboxprotocol.Response{}, fmt.Errorf("sandbox execution timed out or was cancelled: %w", ctx.Err())
		}
		return sandboxprotocol.Response{}, fmt.Errorf("rootless Podman execution failed: %w", runErr)
	}
	if cleanupErr != nil {
		return sandboxprotocol.Response{}, errors.New("rootless Podman cleanup failed")
	}
	stdout, stderr, truncated := streams.result()
	return sandboxprotocol.Response{
		ExitCode: code, Stdout: stdout, Stderr: stderr,
		DurationMS: duration.Milliseconds(), Truncated: truncated,
	}, nil
}

func (p *podmanEngine) capture(ctx context.Context, argv []string, limit int) (string, error) {
	streams := newBoundedStreams(limit)
	code, err := p.run(ctx, argv, streams.stdout(), streams.stderr())
	stdout, _, truncated := streams.result()
	if err != nil || code != 0 || truncated {
		return "", errors.New("Podman attestation command failed")
	}
	return stdout, nil
}

func podmanContainerName(key string) string {
	return "aeontra-l3-" + strings.TrimPrefix(key, "sx_")
}

func podmanRunArgv(spec RunSpec, uid, gid int) []string {
	workdir := "/workspace"
	if spec.RelativeDir != "" && spec.RelativeDir != "." {
		workdir += "/" + strings.TrimPrefix(filepath.ToSlash(spec.RelativeDir), "/")
	}
	cpus := strconv.FormatFloat(float64(spec.CPUMillis)/1000, 'f', 3, 64)
	arguments := []string{
		"run", "--rm", "--name", podmanContainerName(spec.IdempotencyKey),
		"--pull", "never", "--network", "none", "--read-only",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=256m",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--pids-limit", strconv.Itoa(spec.ProcessLimit),
		"--memory", strconv.Itoa(spec.MemoryMiB) + "m", "--cpus", cpus,
		"--userns", "keep-id", "--user", fmt.Sprintf("%d:%d", uid, gid),
		"--pid", "private", "--ipc", "private",
		"--mount", "type=bind,src=" + spec.WorkspaceRoot + ",dst=/workspace,rw",
		"--workdir", workdir,
		"--env", "HOME=/tmp/home", "--env", "TMPDIR=/tmp",
		"--env", "XDG_CONFIG_HOME=/tmp/config", "--env", "XDG_CACHE_HOME=/tmp/cache",
	}
	keys := make([]string, 0, len(spec.Environment))
	for key := range spec.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		arguments = append(arguments, "--env", key+"="+spec.Environment[key])
	}
	arguments = append(arguments, spec.Image)
	arguments = append(arguments, spec.Argv...)
	return arguments
}

func realPodmanCommand(binary, socket string) podmanCommand {
	return func(ctx context.Context, argv []string, stdout, stderr io.Writer) (int, error) {
		arguments := append([]string{"--url", "unix://" + socket}, argv...)
		command := exec.CommandContext(ctx, binary, arguments...)
		command.Stdout = stdout
		command.Stderr = stderr
		err := command.Run()
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode(), nil
		}
		if err != nil {
			return -1, err
		}
		return command.ProcessState.ExitCode(), nil
	}
}

type boundedStreams struct {
	mu        sync.Mutex
	remaining int
	stdoutBuf bytes.Buffer
	stderrBuf bytes.Buffer
	truncated bool
}

type boundedStreamWriter struct {
	streams *boundedStreams
	stdout  bool
}

func newBoundedStreams(limit int) *boundedStreams {
	return &boundedStreams{remaining: limit}
}

func (b *boundedStreams) stdout() io.Writer { return boundedStreamWriter{streams: b, stdout: true} }
func (b *boundedStreams) stderr() io.Writer { return boundedStreamWriter{streams: b, stdout: false} }

func (w boundedStreamWriter) Write(input []byte) (int, error) {
	w.streams.mu.Lock()
	defer w.streams.mu.Unlock()
	writable := len(input)
	if writable > w.streams.remaining {
		writable = w.streams.remaining
		w.streams.truncated = true
	}
	if writable < len(input) {
		w.streams.truncated = true
	}
	if writable > 0 {
		if w.stdout {
			_, _ = w.streams.stdoutBuf.Write(input[:writable])
		} else {
			_, _ = w.streams.stderrBuf.Write(input[:writable])
		}
		w.streams.remaining -= writable
	}
	return len(input), nil
}

func (b *boundedStreams) result() (string, string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stdoutBuf.String(), b.stderrBuf.String(), b.truncated
}
