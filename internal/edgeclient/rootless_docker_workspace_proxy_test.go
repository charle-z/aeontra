//go:build !windows

package edgeclient

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRewriteDockerCreateWorkspaceBinds(t *testing.T) {
	workspace := t.TempDir()
	aliasDir := t.TempDir()
	reports := filepath.Join(workspace, "bin", "testreports")
	if err := os.MkdirAll(reports, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := createDockerWorkspaceAlias(workspace, aliasDir); err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"Image":"example","HostConfig":{"Binds":["/workspace/bin/testreports:/testreports:rw","/tmp/other:/other:ro"],"Mounts":[{"Type":"bind","Source":"/workspace/bin/testreports","Target":"/reports","ReadOnly":true}]}}`)
	output, err := rewriteDockerCreateWorkspaceBinds(input, workspace, aliasDir)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Image      string
		HostConfig struct {
			Binds  []string
			Mounts []struct {
				Source   string
				ReadOnly bool
			}
		}
	}
	if err := json.Unmarshal(output, &created); err != nil {
		t.Fatal(err)
	}
	if created.Image != "example" || len(created.HostConfig.Binds) != 2 || len(created.HostConfig.Mounts) != 1 || !created.HostConfig.Mounts[0].ReadOnly {
		t.Fatalf("unrelated create fields changed: %+v", created)
	}
	if created.HostConfig.Binds[1] != "/tmp/other:/other:ro" {
		t.Fatalf("unrelated bind changed: %q", created.HostConfig.Binds[1])
	}
	alias := strings.SplitN(created.HostConfig.Binds[0], ":", 2)[0]
	if alias != created.HostConfig.Mounts[0].Source || !pathInside(aliasDir, alias) || strings.Contains(string(output), workspace) {
		t.Fatalf("workspace bind did not use the same opaque alias: %q", output)
	}
	resolved, err := filepath.EvalSymlinks(alias)
	if err != nil || resolved != reports {
		t.Fatalf("alias target=%q err=%v want=%q", resolved, err, reports)
	}
}

func TestRewriteDockerCreateRejectsWorkspaceEscape(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "escape")); err != nil {
		t.Fatal(err)
	}
	aliasDir := t.TempDir()
	if err := createDockerWorkspaceAlias(workspace, aliasDir); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"/workspace/../etc", "/workspace/escape", "/workspace/missing"} {
		input := []byte(`{"HostConfig":{"Binds":["` + source + `:/target"]}}`)
		if _, err := rewriteDockerCreateWorkspaceBinds(input, workspace, aliasDir); err == nil {
			t.Fatalf("unsafe workspace bind %q was accepted", source)
		}
	}
}

func TestRewriteDockerCreateLeavesOtherBindsUnchanged(t *testing.T) {
	input := []byte(`{"HostConfig":{"Binds":["/tmp/source:/target"],"Mounts":[{"Type":"volume","Source":"cache","Target":"/cache"}]}}`)
	output, err := rewriteDockerCreateWorkspaceBinds(input, t.TempDir(), t.TempDir())
	if err != nil || string(output) != string(input) {
		t.Fatalf("unrelated create request changed: %q err=%v", output, err)
	}
}

func TestDockerWorkspaceAliasStaysOutsideMountedRuntime(t *testing.T) {
	workspace := t.TempDir()
	socketRoot := t.TempDir()
	runtimeDir := filepath.Join(socketRoot, "runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasDir, err := prepareDockerWorkspaceAlias(workspace, runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(aliasDir)
	if !pathInside(socketRoot, aliasDir) || pathInside(runtimeDir, aliasDir) {
		t.Fatalf("Docker alias %q must be outside mounted runtime %q", aliasDir, runtimeDir)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(aliasDir, "workspace"))
	if err != nil || resolved != workspace {
		t.Fatalf("Docker alias target=%q err=%v want=%q", resolved, err, workspace)
	}
}

func TestRootlessDockerWorkspaceProxyRealEngine(t *testing.T) {
	if os.Getenv("CODEX_ROOTLESS_DOCKER_PROXY_E2E") != "1" {
		t.Skip("real Docker rootless workspace bind acceptance is explicit")
	}
	uid := os.Geteuid()
	endpoint := RootlessContainerEndpoint{Engine: "docker", SocketPath: filepath.Join("/run/user", strconv.Itoa(uid), "docker.sock")}
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "bin", "testreports"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "bin", "testreports", "proof"), []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socketRoot := t.TempDir()
	runtimeDir := filepath.Join(socketRoot, "runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	proxySocket, done, cleanup, err := startRootlessDockerWorkspaceProxy(ctx, endpoint, workspace, runtimeDir, uid)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	defer func() { cancel(); <-done }()
	aliases, err := filepath.Glob(filepath.Join(socketRoot, "mcp-devbox-bind-*", "workspace"))
	if err != nil || len(aliases) != 1 || pathInside(runtimeDir, aliases[0]) {
		t.Fatalf("workspace alias must live outside the mounted runtime: aliases=%v err=%v", aliases, err)
	}
	docker := os.Getenv("CODEX_DOCKER_CLIENT")
	if docker == "" {
		t.Fatal("CODEX_DOCKER_CLIENT must name the pinned Docker client")
	}
	for _, mountArgs := range [][]string{
		{"-v", "/workspace/bin/testreports:/testreports:ro"},
		{"--mount", "type=bind,source=/workspace/bin/testreports,target=/testreports,readonly"},
	} {
		args := append([]string{"run", "--rm", "--pull=never"}, mountArgs...)
		args = append(args, "alpine:latest", "cat", "/testreports/proof")
		command := exec.CommandContext(ctx, docker, args...)
		command.Env = append(os.Environ(), "DOCKER_HOST=unix://"+proxySocket)
		output, err := command.CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != "ok" {
			t.Fatalf("Docker create/start/attach through proxy failed for %v: err=%v output=%q", mountArgs, err, output)
		}
	}
}
