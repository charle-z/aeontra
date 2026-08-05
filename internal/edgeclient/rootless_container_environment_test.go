//go:build !windows

package edgeclient

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRootlessContainerEnvironment(endpoint *RootlessContainerEndpoint, toolPath string) ([]string, error) {
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(home) == "" {
		home = "/nonexistent"
	}
	socket := ""
	if endpoint != nil {
		socket = filepath.Clean(endpoint.SocketPath)
	}
	return []string{
		"PATH=" + toolPath,
		"HOME=" + home,
		"LANG=C",
		"LC_ALL=C",
		"XDG_RUNTIME_DIR=/run/user/test",
		"CONTAINER_HOST=unix://" + socket,
		"DOCKER_HOST=unix://" + socket,
	}, nil
}

func testOwnedRootlessSocket(t *testing.T) (string, string) {
	t.Helper()
	runtimeRoot := t.TempDir()
	socketDir := filepath.Join(runtimeRoot, "podman")
	if err := os.Mkdir(socketDir, 0o700); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(socketDir, "podman.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatal(err)
	}
	return runtimeRoot, socketPath
}

func environmentMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, item, ok := strings.Cut(value, "=")
		if ok {
			result[key] = item
		}
	}
	return result
}

func TestRootlessContainerClientEnvironmentUsesExactValidatedUnixSocket(t *testing.T) {
	runtimeRoot, socketPath := testOwnedRootlessSocket(t)
	endpoint := &RootlessContainerEndpoint{Engine: "podman", SocketPath: socketPath, Executable: "/usr/bin/podman"}
	environment, err := rootlessContainerClientEnvironmentFor(endpoint, openCodeDefaultToolPath, runtimeRoot, os.Geteuid())
	if err != nil {
		t.Fatal(err)
	}
	values := environmentMap(environment)
	expected := "unix://" + socketPath
	if values["CONTAINER_HOST"] != expected || values["DOCKER_HOST"] != expected {
		t.Fatalf("container endpoints=%q/%q want=%q", values["CONTAINER_HOST"], values["DOCKER_HOST"], expected)
	}
	if values["XDG_RUNTIME_DIR"] != runtimeRoot || values["PATH"] != openCodeDefaultToolPath || values["HOME"] == "" || values["LANG"] != "C" || values["LC_ALL"] != "C" {
		t.Fatalf("environment=%q", environment)
	}
	if len(values) != 7 {
		t.Fatalf("environment keys=%v", values)
	}
	if strings.Contains(strings.Join(environment, "\n"), "/var/run/docker.sock") || strings.Contains(strings.Join(environment, "\n"), "/run/docker.sock") {
		t.Fatalf("rootful fallback present: %q", environment)
	}
}

func TestRootlessContainerClientEnvironmentRejectsInvalidSockets(t *testing.T) {
	runtimeRoot := t.TempDir()
	socketDir := filepath.Join(runtimeRoot, "podman")
	if err := os.Mkdir(socketDir, 0o700); err != nil {
		t.Fatal(err)
	}
	endpoint := func(path string) *RootlessContainerEndpoint {
		return &RootlessContainerEndpoint{Engine: "podman", SocketPath: path, Executable: "/usr/bin/podman"}
	}
	type testCase struct {
		name     string
		endpoint *RootlessContainerEndpoint
		root     string
		uid      int
	}
	cases := []testCase{
		{"absent", endpoint(filepath.Join(socketDir, "missing.sock")), runtimeRoot, os.Geteuid()},
		{"non-unix-url", endpoint("tcp://127.0.0.1:2375"), runtimeRoot, os.Geteuid()},
		{"rootful-fallback", endpoint("/var/run/docker.sock"), "/var/run", os.Geteuid()},
	}
	regular := filepath.Join(socketDir, "regular.sock")
	if err := os.WriteFile(regular, []byte("not-a-socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases = append(cases, testCase{"regular-file", endpoint(regular), runtimeRoot, os.Geteuid()})

	target := t.TempDir()
	link := filepath.Join(runtimeRoot, "linked")
	if err := os.Symlink(target, link); err == nil {
		cases = append(cases, testCase{"symlink-parent", endpoint(filepath.Join(link, "podman.sock")), runtimeRoot, os.Geteuid()})
	}

	ownedRoot, ownedSocket := testOwnedRootlessSocket(t)
	cases = append(cases, testCase{"wrong-owner", endpoint(ownedSocket), ownedRoot, os.Geteuid() + 1})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := rootlessContainerClientEnvironmentFor(tc.endpoint, openCodeDefaultToolPath, tc.root, tc.uid); err == nil {
				t.Fatalf("invalid endpoint accepted: %+v", tc.endpoint)
			}
		})
	}
}

func TestRootlessContainerClientEnvironmentPreservesBaseEnvironmentWithoutEndpoint(t *testing.T) {
	environment, err := rootlessContainerClientEnvironmentFor(nil, openCodeDefaultToolPath, t.TempDir(), os.Geteuid())
	if err != nil {
		t.Fatal(err)
	}
	values := environmentMap(environment)
	if len(values) != 4 || values["PATH"] != openCodeDefaultToolPath || values["HOME"] == "" || values["LANG"] != "C" || values["LC_ALL"] != "C" {
		t.Fatalf("base environment=%q", environment)
	}
	for _, key := range []string{"XDG_RUNTIME_DIR", "CONTAINER_HOST", "DOCKER_HOST"} {
		if _, ok := values[key]; ok {
			t.Fatalf("unexpected %s in base environment", key)
		}
	}
}
