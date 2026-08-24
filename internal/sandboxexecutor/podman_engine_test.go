package sandboxexecutor

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

func TestPodmanArgvContainsOnlyReviewedAuthority(t *testing.T) {
	spec := RunSpec{
		WorkspaceRoot: "C:\\work\\repo", RelativeDir: "sub", Argv: []string{"bash", "-lc", "cargo test && cargo fmt --check"},
		Environment: map[string]string{"CI": "1"}, NetworkProfile: "none", Timeout: time.Minute,
		CPUMillis: 750, MemoryMiB: 768, ProcessLimit: 128, OutputBytes: 4096,
		Image: "localhost/aeontra-l3@" + executorTestDigest, IdempotencyKey: "sx_0123456789abcdef0123456789abcdef",
	}
	argv := podmanRunArgv(spec, 1001, 1001)
	joined := strings.Join(argv, " ")
	for _, expected := range []string{
		"run --rm", "--network none", "--read-only", "--cap-drop ALL",
		"--security-opt no-new-privileges", "--pids-limit 128", "--memory 768m",
		"--cpus 0.750", "--userns keep-id", "--user 1001:1001", "--pid private", "--ipc private",
		"dst=/workspace,rw", "--workdir /workspace/sub", "--env CI=1",
		"HOME=/tmp/home", "TMPDIR=/tmp", spec.Image,
	} {
		if !strings.Contains(joined, expected) {
			t.Errorf("podman argv missing %q: %s", expected, joined)
		}
	}
	for _, forbidden := range []string{"--privileged", "docker.sock", "podman.sock", "--network host", "--pid host", "--ipc host", "--device", "--cap-add"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("podman argv contains forbidden authority %q: %s", forbidden, joined)
		}
	}
	if !strings.HasSuffix(joined, spec.Image+" bash -lc cargo test && cargo fmt --check") {
		t.Fatalf("image and literal command must be final argv: %s", joined)
	}
}

func TestPodmanEngineBoundsCombinedOutputAndCleansUp(t *testing.T) {
	var calls [][]string
	engine := &podmanEngine{
		socket: "/run/user/1001/podman/podman.sock", binary: "/usr/bin/podman", uid: 1001, gid: 1001,
		run: func(_ context.Context, argv []string, stdout, stderr io.Writer) (int, error) {
			calls = append(calls, append([]string(nil), argv...))
			if len(argv) > 0 && argv[0] == "run" {
				_, _ = stdout.Write([]byte(strings.Repeat("o", 12)))
				_, _ = stderr.Write([]byte(strings.Repeat("e", 12)))
				return 0, nil
			}
			return 0, nil
		},
	}
	response, err := engine.Run(context.Background(), RunSpec{
		WorkspaceRoot: t.TempDir(), Argv: []string{"true"}, NetworkProfile: "none",
		Timeout: time.Second, CPUMillis: 1000, MemoryMiB: 128, ProcessLimit: 16,
		OutputBytes: 16, Image: "image@" + executorTestDigest,
		IdempotencyKey: "sx_0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Truncated || len(response.Stdout)+len(response.Stderr) != 16 {
		t.Fatalf("combined output was not bounded: %#v", response)
	}
	if len(calls) != 2 || calls[1][0] != "rm" || calls[1][1] != "--force" {
		t.Fatalf("deterministic cleanup missing: %#v", calls)
	}
}

func TestPodmanEngineAttestationRejectsRootlessOrImageDrift(t *testing.T) {
	for name, outputs := range map[string][]string{
		"not rootless": {"false\n", executorTestDigest + "\n"},
		"image drift":  {"true\n", "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff\n"},
	} {
		t.Run(name, func(t *testing.T) {
			index := 0
			engine := &podmanEngine{socket: "/run/user/1001/podman/podman.sock", binary: "/usr/bin/podman", uid: 1001, gid: 1001,
				run: func(_ context.Context, _ []string, stdout, _ io.Writer) (int, error) {
					if index >= len(outputs) {
						return 1, errors.New("unexpected call")
					}
					_, _ = io.WriteString(stdout, outputs[index])
					index++
					return 0, nil
				},
			}
			if err := engine.Attest(context.Background(), "image@"+executorTestDigest, executorTestDigest); err == nil {
				t.Fatal("drifting engine was accepted")
			}
		})
	}
}

var _ Engine = (*podmanEngine)(nil)
var _ = sandboxprotocol.Response{}
