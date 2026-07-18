//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type recordedContainerCommand struct {
	executable string
	args       []string
	env        []string
}

type fakeContainerRunner struct {
	commands []recordedContainerCommand
}

func (r *fakeContainerRunner) Run(_ context.Context, executable string, args, env []string) ([]byte, error) {
	r.commands = append(r.commands, recordedContainerCommand{executable: executable, args: append([]string(nil), args...), env: append([]string(nil), env...)})
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, " ps "):
		return []byte("c2\nc1\n"), nil
	case strings.Contains(joined, " network ls "):
		return []byte("n1\n"), nil
	case strings.Contains(joined, " volume ls "):
		return []byte("v1\n"), nil
	default:
		return nil, nil
	}
}

func TestValidateRootlessContainerSocketAcceptsOwnedUnixSocket(t *testing.T) {
	runtimeRoot := t.TempDir()
	path := filepath.Join(runtimeRoot, "docker.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRootlessContainerSocket(path, runtimeRoot, os.Geteuid()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRootlessContainerSocketRejectsRootfulAndSymlinkedPaths(t *testing.T) {
	if err := validateRootlessContainerSocket("/var/run/docker.sock", "/var/run", os.Geteuid()); err == nil {
		t.Fatal("rootful docker socket accepted")
	}
	runtimeRoot := t.TempDir()
	target := t.TempDir()
	link := filepath.Join(runtimeRoot, "podman")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := validateRootlessContainerSocket(filepath.Join(link, "podman.sock"), runtimeRoot, os.Geteuid()); err == nil {
		t.Fatal("symlinked rootless socket parent accepted")
	}
}

func TestCleanupRootlessContainerResourcesUsesExactRuntimeLabel(t *testing.T) {
	runner := &fakeContainerRunner{}
	endpoint := &RootlessContainerEndpoint{Engine: "docker", SocketPath: "/run/user/1000/docker.sock", Executable: "/usr/bin/docker"}
	const runtimeID = "mr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := CleanupRootlessContainerResources(context.Background(), endpoint, runtimeID, openCodeDefaultToolPath, runner); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 6 {
		t.Fatalf("commands=%d %#v", len(runner.commands), runner.commands)
	}
	label := "label=" + rootlessRuntimeLabelKey + "=" + runtimeID
	for _, index := range []int{0, 2, 4} {
		if !strings.Contains(strings.Join(runner.commands[index].args, " "), label) {
			t.Fatalf("list command missing exact label: %#v", runner.commands[index])
		}
	}
	if got := runner.commands[1].args[len(runner.commands[1].args)-2:]; !reflect.DeepEqual(got, []string{"c1", "c2"}) {
		t.Fatalf("container cleanup ids=%v", got)
	}
}

func TestCleanupRootlessContainerResourcesRejectsUnsafeEngineOutput(t *testing.T) {
	runner := &unsafeContainerRunner{}
	endpoint := &RootlessContainerEndpoint{Engine: "podman", SocketPath: "/run/user/1000/podman/podman.sock", Executable: "/usr/bin/podman"}
	if err := CleanupRootlessContainerResources(context.Background(), endpoint, "mr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", openCodeDefaultToolPath, runner); err == nil {
		t.Fatal("unsafe resource id accepted")
	}
}

type unsafeContainerRunner struct{}

func (*unsafeContainerRunner) Run(context.Context, string, []string, []string) ([]byte, error) {
	return []byte("../../escape\n"), nil
}

func TestCleanupRootlessContainerResourcesRejectsInvalidRuntime(t *testing.T) {
	endpoint := &RootlessContainerEndpoint{Engine: "docker", SocketPath: "/run/user/1000/docker.sock", Executable: "/usr/bin/docker"}
	if err := CleanupRootlessContainerResources(context.Background(), endpoint, "invalid", openCodeDefaultToolPath, &fakeContainerRunner{}); err == nil {
		t.Fatal("invalid runtime accepted")
	}
	if err := CleanupRootlessContainerResources(context.Background(), nil, "invalid", openCodeDefaultToolPath, nil); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("nil endpoint should require no cleanup: %v", err)
	}
}
