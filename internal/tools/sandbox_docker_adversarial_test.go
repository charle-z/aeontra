package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Part 1: argv-contract adversarial tests. These run everywhere and prove the
// sandbox invocation is hardened by CONSTRUCTION — a malicious command cannot
// widen the container's powers, because containment comes from the docker flags,
// not from trying to sanitize the command text.
// ---------------------------------------------------------------------------

func TestDockerArgv_EnvIsAllowlistOnly(t *testing.T) {
	r := NewDockerSandboxRunner(DockerSandboxConfig{Image: "img", Root: "/r"}).(dockerSandboxRunner)
	argv := dockerArgv(r.cfg, SandboxRunRequest{
		Dir:          "/r",
		Argv:         []string{"env"},
		EnvAllowlist: map[string]string{"CI": "1", "GOFLAGS": "-mod=mod"},
	})
	joined := strings.Join(argv, " ")
	// Exactly the allowlisted vars are injected...
	if !strings.Contains(joined, "-e CI=1") || !strings.Contains(joined, "-e GOFLAGS=-mod=mod") {
		t.Fatalf("allowlisted env not injected: %s", joined)
	}
	// ...and no host / sensitive env is ever forwarded.
	for _, leak := range []string{"MCP_DEVBOX_TOKEN", "-e PATH=", "-e HOME=", "AWS_", "SECRET"} {
		if strings.Contains(joined, leak) {
			t.Errorf("host/sensitive env leaked into sandbox argv: %q in %s", leak, joined)
		}
	}
}

func TestDockerArgv_MaliciousCommandStaysContained(t *testing.T) {
	// A nasty command is passed literally as the trailing argv (never a shell that
	// the runner interprets) and is still wrapped by full isolation.
	r := NewDockerSandboxRunner(DockerSandboxConfig{Image: "alpine", Root: "/work"}).(dockerSandboxRunner)
	argv := dockerArgv(r.cfg, SandboxRunRequest{
		Dir:  "/work",
		Argv: []string{"sh", "-c", "cat /etc/shadow; wget http://169.254.169.254/"},
	})
	joined := strings.Join(argv, " ")
	for _, must := range []string{"--network none", "--read-only", "--cap-drop ALL", "--security-opt no-new-privileges", "--user 10001:10001"} {
		if !strings.Contains(joined, must) {
			t.Errorf("isolation flag missing even for a malicious command: %q\n%s", must, joined)
		}
	}
	if !strings.HasSuffix(joined, "alpine sh -c cat /etc/shadow; wget http://169.254.169.254/") {
		t.Errorf("command must be the literal trailing argv: %s", joined)
	}
}

func TestDockerArgv_MountsOnlyTheWorkspace(t *testing.T) {
	r := NewDockerSandboxRunner(DockerSandboxConfig{Image: "img", Root: "/repos/only"}).(dockerSandboxRunner)
	argv := dockerArgv(r.cfg, SandboxRunRequest{Dir: "/repos/only", Argv: []string{"ls"}})
	mounts := 0
	for i, a := range argv {
		switch a {
		case "-v", "--volume":
			mounts++
		case "--mount":
			mounts++
			if i+1 >= len(argv) || !strings.Contains(argv[i+1], "src=/repos/only,dst=/repos/only") {
				t.Errorf("the only mount must be the workspace, got %v", argv)
			}
		}
	}
	if mounts != 1 {
		t.Errorf("exactly one mount (the workspace) expected, got %d: %v", mounts, argv)
	}
}

// ---------------------------------------------------------------------------
// Part 2: real containment tests. These actually run Docker and verify the
// sandbox contains a hostile command. They require Linux + a working Docker
// daemon and are skipped elsewhere (e.g. the Windows dev host). Run them on the
// VPS or in WSL2 before enabling broad command execution (l3-sandbox-plan step 5).
// ---------------------------------------------------------------------------

func requireDockerSandbox(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("docker sandbox containment tests run on Linux only")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker binary not available")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker daemon not available")
	}
}

func realDockerSandbox(root string, timeout time.Duration) SandboxRunner {
	return NewDockerSandboxRunner(DockerSandboxConfig{
		Image:   "alpine:3.22",
		Root:    root,
		Timeout: timeout,
	})
}

func TestDockerSandbox_Integration_RunsBaseline(t *testing.T) {
	requireDockerSandbox(t)
	root := t.TempDir()
	res, err := realDockerSandbox(root, 60*time.Second).Run(context.Background(),
		SandboxRunRequest{Dir: root, Argv: []string{"sh", "-c", "echo hello-sandbox"}})
	if err != nil {
		t.Fatalf("baseline run error: %v", err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "hello-sandbox") {
		t.Fatalf("baseline run failed: %+v", res)
	}
}

func TestDockerSandbox_Integration_EgressDenied(t *testing.T) {
	requireDockerSandbox(t)
	root := t.TempDir()
	sb := realDockerSandbox(root, 30*time.Second)
	// --network none => the metadata endpoint and the internet are unreachable.
	for _, target := range []string{"http://169.254.169.254/latest/meta-data/", "http://1.1.1.1/"} {
		res, _ := sb.Run(context.Background(),
			SandboxRunRequest{Dir: root, Argv: []string{"sh", "-c", "wget -T 3 -q -O- " + target}})
		if res.ExitCode == 0 {
			t.Errorf("egress to %s must be blocked (network none), got exit 0 stdout=%q", target, res.Stdout)
		}
	}
}

func TestDockerSandbox_Integration_HostFilesystemNotVisible(t *testing.T) {
	requireDockerSandbox(t)
	root := t.TempDir()
	// A secret on the host, OUTSIDE the workspace mount, must not be readable inside.
	hostSecret := filepath.Join(filepath.Dir(root), "host-only-secret.txt")
	if err := os.WriteFile(hostSecret, []byte("HOSTSECRET123"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(hostSecret) })
	res, _ := realDockerSandbox(root, 30*time.Second).Run(context.Background(),
		SandboxRunRequest{Dir: root, Argv: []string{"sh", "-c", "cat " + hostSecret}})
	if res.ExitCode == 0 || strings.Contains(res.Stdout, "HOSTSECRET123") {
		t.Errorf("host file outside the workspace must not be readable in the sandbox: %+v", res)
	}
}

func TestDockerSandbox_Integration_RootfsReadOnly(t *testing.T) {
	requireDockerSandbox(t)
	root := t.TempDir()
	// Writing onto the read-only rootfs (outside the rw workspace) must fail.
	res, _ := realDockerSandbox(root, 30*time.Second).Run(context.Background(),
		SandboxRunRequest{Dir: root, Argv: []string{"sh", "-c", "echo pwned > /etc/evil"}})
	if res.ExitCode == 0 {
		t.Errorf("writing to a read-only rootfs must fail: %+v", res)
	}
}

func TestDockerSandbox_Integration_TimeoutKills(t *testing.T) {
	requireDockerSandbox(t)
	root := t.TempDir()
	start := time.Now()
	res, _ := realDockerSandbox(root, 2*time.Second).Run(context.Background(),
		SandboxRunRequest{Dir: root, Argv: []string{"sh", "-c", "sleep 30"}})
	elapsed := time.Since(start)
	if elapsed > 15*time.Second {
		t.Errorf("timeout did not kill the command promptly (elapsed %v)", elapsed)
	}
	if res.ExitCode == 0 {
		t.Errorf("a timed-out command must not report exit 0: %+v", res)
	}
}
