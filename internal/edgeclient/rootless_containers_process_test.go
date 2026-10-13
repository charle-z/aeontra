//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecContainerCommandRunnerIsolatesClientRuntime(t *testing.T) {
	for _, exitCode := range []int{0, 2} {
		t.Run(strconv.Itoa(exitCode), func(t *testing.T) {
			hostRuntime := filepath.Join(t.TempDir(), "absent-host-runtime")
			socket := "unix://" + filepath.Join(hostRuntime, "podman", "podman.sock")
			environment := []string{"PATH=/usr/bin:/bin", "XDG_RUNTIME_DIR=" + hostRuntime, "CONTAINER_HOST=" + socket, "DOCKER_HOST=" + socket}
			original := append([]string(nil), environment...)
			output, err := (execContainerCommandRunner{}).Run(t.Context(), "/bin/sh", []string{"-c", `
test -d "$XDG_RUNTIME_DIR" || exit 91
test "$(stat -c %a "$XDG_RUNTIME_DIR")" = 700 || exit 92
mkdir "$XDG_RUNTIME_DIR/libpod" || exit 93
printf '%s\n%s\n%s\n' "$XDG_RUNTIME_DIR" "$CONTAINER_HOST" "$DOCKER_HOST"
exit "$1"`, "client-test", strconv.Itoa(exitCode)}, environment)
			if exitCode == 0 && err != nil {
				t.Fatal(err)
			}
			if exitCode != 0 {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != exitCode {
					t.Fatalf("exit error=%v output=%q", err, output)
				}
			}
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) != 3 || lines[0] == hostRuntime || lines[1] != socket || lines[2] != socket {
				t.Fatalf("client environment=%q", output)
			}
			if _, err := os.Stat(lines[0]); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("client runtime survived: %v", err)
			}
			if _, err := os.Stat(hostRuntime); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("host runtime changed: %v", err)
			}
			if !reflect.DeepEqual(environment, original) {
				t.Fatal("caller environment mutated")
			}
		})
	}
}

func TestExecContainerCommandRunnerKeepsStderrOutOfMachineOutput(t *testing.T) {
	output, err := execContainerCommandRunner{}.Run(
		t.Context(),
		"/bin/sh",
		[]string{"-c", `printf 'resource-id\n'; printf 'warning from engine\n' >&2`},
		[]string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(output); got != "resource-id\n" {
		t.Fatalf("machine output=%q", got)
	}
}

func TestExecContainerCommandRunnerCleansRuntimeOnStartFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	_, err := (execContainerCommandRunner{}).Run(t.Context(), filepath.Join(root, "missing-client"), nil, []string{"XDG_RUNTIME_DIR=/missing-host-runtime"})
	if err == nil {
		t.Fatal("missing executable succeeded")
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("scratch survived failed start: entries=%v err=%v", entries, readErr)
	}
}

func TestExecContainerCommandRunnerPreservesAuthLocation(t *testing.T) {
	runtime := t.TempDir()
	if err := os.Mkdir(filepath.Join(runtime, "containers"), 0o700); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(runtime, "containers", "auth.json")
	if err := os.WriteFile(authPath, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, explicit := range []bool{false, true} {
		environment := []string{"PATH=/usr/bin:/bin", "XDG_RUNTIME_DIR=" + runtime}
		want := authPath
		if explicit {
			want = filepath.Join(t.TempDir(), "explicit-auth.json")
			environment = append(environment, "REGISTRY_AUTH_FILE="+want)
		}
		output, err := (execContainerCommandRunner{}).Run(t.Context(), "/bin/sh", []string{"-c", `test ! -e "$XDG_RUNTIME_DIR/containers/auth.json" && printf '%s' "$REGISTRY_AUTH_FILE"`}, environment)
		if err != nil || string(output) != want {
			t.Fatalf("auth location changed: output=%q err=%v", output, err)
		}
	}
	if content, err := os.ReadFile(authPath); err != nil || string(content) != "fixture" {
		t.Fatalf("original auth modified: %v", err)
	}
}

func TestExecContainerCommandRunnerRejectsUnavailableScratch(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "absent"))
	_, err := (execContainerCommandRunner{}).Run(t.Context(), "/bin/true", nil, []string{"XDG_RUNTIME_DIR=/missing-host-runtime"})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch failure did not stop execution: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = (execContainerCommandRunner{}).Run(ctx, "/bin/true", nil, []string{"XDG_RUNTIME_DIR=/missing-host-runtime"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-start cancellation lost precedence: %v", err)
	}
}

func TestExecContainerCommandRunnerConcurrentClientRuntimes(t *testing.T) {
	paths := make(chan string, 8)
	for i := 0; i < cap(paths); i++ {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			output, err := (execContainerCommandRunner{}).Run(t.Context(), "/bin/sh", []string{"-c", `mkdir "$XDG_RUNTIME_DIR/libpod" && printf '%s' "$XDG_RUNTIME_DIR"`}, []string{"PATH=/usr/bin:/bin", "XDG_RUNTIME_DIR=/missing-host-runtime"})
			if err != nil {
				t.Fatal(err)
			}
			path := string(output)
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("runtime survived: %v", err)
			}
			paths <- path
		})
	}
	t.Cleanup(func() {
		close(paths)
		seen := map[string]bool{}
		for path := range paths {
			if seen[path] {
				t.Fatal("concurrent calls shared client runtime")
			}
			seen[path] = true
		}
	})
}

func TestExecContainerCommandRunnerPreservesFailedCommandOutput(t *testing.T) {
	output, err := execContainerCommandRunner{}.Run(
		t.Context(), "/bin/sh", []string{"-c", `printf 'test started\n'; printf 'assertion failed\n' >&2; exit 2`},
		[]string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"},
	)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 || string(output) != "test started\nassertion failed\n" {
		t.Fatalf("output=%q err=%v", output, err)
	}
}

func TestExecContainerCommandRunnerBoundsFailedCommandOutput(t *testing.T) {
	output, err := execContainerCommandRunner{}.Run(
		t.Context(), "/bin/sh", []string{"-c", `printf '%070000d' 0; printf 'failure-tail\n' >&2; exit 2`},
		[]string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"},
	)
	var limitErr *rootlessCommandOutputLimitError
	if !errors.As(err, &limitErr) || limitErr.ExitCode() != 2 || len(output) != 64<<10 || !strings.HasSuffix(string(output), "failure-tail\n") {
		t.Fatalf("length=%d tail=%q err=%v", len(output), string(output[max(0, len(output)-32):]), err)
	}
}

func TestExecContainerCommandRunnerCancelsWholeProcessGroup(t *testing.T) {
	root := t.TempDir()
	heartbeat := filepath.Join(root, "heartbeat")
	pidPath := filepath.Join(root, "child.pid")
	runtimePath := filepath.Join(root, "client-runtime")
	script := `heartbeat=$1; pidfile=$2; printf '%s' "$XDG_RUNTIME_DIR" >"$3"; (trap '' TERM; while :; do printf x >>"$heartbeat"; sleep 0.05; done) & child=$!; printf '%s\n' "$child" >"$pidfile"; wait "$child"`

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := execContainerCommandRunner{}.Run(ctx, "/bin/sh", []string{"-c", script, "p12", heartbeat, pidPath, runtimePath}, []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "XDG_RUNTIME_DIR=/missing-host-runtime"})
		done <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	var childPID int
	for time.Now().Before(deadline) {
		pidBody, pidErr := os.ReadFile(pidPath)
		info, heartbeatErr := os.Stat(heartbeat)
		if pidErr == nil && heartbeatErr == nil && info.Size() >= 3 {
			parsed, parseErr := strconv.Atoi(strings.TrimSpace(string(pidBody)))
			if parseErr == nil && parsed > 1 {
				childPID = parsed
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if childPID == 0 {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		t.Fatal("child process did not become ready")
	}
	t.Cleanup(func() { _ = syscall.Kill(childPID, syscall.SIGKILL) })

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation cause lost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rootless command did not return after cancellation")
	}
	runtime, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(runtime)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime survived cancellation: %v", err)
	}

	time.Sleep(150 * time.Millisecond)
	before, err := os.Stat(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)
	after, err := os.Stat(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("child process survived cancellation: heartbeat grew from %d to %d", before.Size(), after.Size())
	}
}
