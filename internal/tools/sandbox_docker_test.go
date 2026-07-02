package tools

import (
	"context"
	"strings"
	"testing"
)

func TestDockerArgv_HardeningFlags(t *testing.T) {
	cfg := DockerSandboxConfig{Image: "golang:1.26-alpine", Root: "/repos/proj"}
	// fill defaults via constructor path
	r := NewDockerSandboxRunner(cfg).(dockerSandboxRunner)
	argv := dockerArgv(r.cfg, SandboxRunRequest{Dir: "/repos/proj/sub", Argv: []string{"go", "build", "./..."}})
	joined := strings.Join(argv, " ")

	mustContain := []string{
		"run --rm",
		"--network none", // egress default-deny
		"--read-only",    // read-only rootfs
		"--cap-drop ALL", // no capabilities
		"--security-opt no-new-privileges",
		"--pids-limit 256",
		"--memory 512m",
		"--user 10001:10001", // non-root
		"--workdir /repos/proj/sub",
		"type=bind,src=/repos/proj,dst=/repos/proj",
		"golang:1.26-alpine go build ./...", // image then argv, last
	}
	for _, w := range mustContain {
		if !strings.Contains(joined, w) {
			t.Errorf("docker argv missing %q\nfull: %s", w, joined)
		}
	}
	// The command argv must come AFTER the image (nothing hardening-related after it).
	if !strings.HasSuffix(joined, "golang:1.26-alpine go build ./...") {
		t.Errorf("image+argv must be last: %s", joined)
	}
}

func TestDockerArgv_NeverGrantsEscapePrimitives(t *testing.T) {
	r := NewDockerSandboxRunner(DockerSandboxConfig{Image: "img", Root: "/r"}).(dockerSandboxRunner)
	argv := dockerArgv(r.cfg, SandboxRunRequest{Dir: "/r", Argv: []string{"sh"}})
	joined := strings.Join(argv, " ")
	for _, forbidden := range []string{
		"--privileged",
		"docker.sock",
		"/var/run/docker.sock",
		"--network host",
		"--cap-add",
		"--pid host",
		"--userns host",
		"-v /:",
	} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("docker argv must never contain escape primitive %q: %s", forbidden, joined)
		}
	}
}

func TestDockerSandbox_RunUsesArgvAndReturnsResult(t *testing.T) {
	var gotArgs []string
	fake := func(ctx context.Context, args []string) (string, string, int, error) {
		gotArgs = args
		return "build ok", "warn", 0, nil
	}
	cfg := DockerSandboxConfig{Image: "img", Root: "/r", exec: fake}
	r := NewDockerSandboxRunner(cfg)
	res, err := r.Run(context.Background(), SandboxRunRequest{Dir: "/r", Argv: []string{"go", "test"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.Stdout != "build ok" || res.Stderr != "warn" {
		t.Errorf("unexpected result: %+v", res)
	}
	if res.SandboxBackend != "docker" || res.EgressProfile != "none" {
		t.Errorf("result metadata wrong: %+v", res)
	}
	if len(gotArgs) == 0 || gotArgs[0] != "run" {
		t.Errorf("exec should receive the docker run argv, got %v", gotArgs)
	}
	if strings.Join(gotArgs, " ") == "" || !strings.Contains(strings.Join(gotArgs, " "), "go test") {
		t.Errorf("argv should include the command: %v", gotArgs)
	}
}

func TestDockerSandbox_StatusReportsHardenedButUnverified(t *testing.T) {
	r := NewDockerSandboxRunner(DockerSandboxConfig{Image: "img", Root: "/r"})
	st := r.Status(context.Background())
	if st.Backend != "docker" || st.FreeTerminal {
		t.Errorf("status wrong: %+v", st)
	}
	if st.DefaultEgress != "none" {
		t.Errorf("default egress should be none (deny): %+v", st)
	}
	out := strings.ToLower(formatSandboxStatus(st))
	if !strings.Contains(out, "verified") { // must flag verification-pending
		t.Errorf("status should flag adversarial verification pending: %s", out)
	}
}
