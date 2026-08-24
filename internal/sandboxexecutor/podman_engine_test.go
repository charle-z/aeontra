package sandboxexecutor

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func apiResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func podmanLogFrame(stream byte, body string) string {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(body)))
	return string(header) + body
}

func testPodmanEngine(handler roundTripFunc) *podmanEngine {
	return &podmanEngine{
		socket: "/run/user/1001/podman/podman.sock",
		uid:    1001,
		gid:    1001,
		client: &http.Client{Transport: handler},
	}
}

func TestPodmanCreateSpecContainsOnlyReviewedAuthority(t *testing.T) {
	spec := RunSpec{
		WorkspaceRoot: "/srv/work/repo", RelativeDir: "sub", Argv: []string{"bash", "-lc", "cargo test && cargo fmt --check"},
		Environment: map[string]string{"CI": "1"}, NetworkProfile: "none", Timeout: time.Minute,
		CPUMillis: 750, MemoryMiB: 768, ProcessLimit: 128, OutputBytes: 4096,
		Image: "localhost/aeontra-l3@" + executorTestDigest, IdempotencyKey: "sx_0123456789abcdef0123456789abcdef",
	}
	created := podmanCreateSpec(spec, 1001, 1001)
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, expected := range []string{
		`"name":"aeontra-l3-0123456789abcdef0123456789abcdef"`,
		`"netns":{"nsmode":"none"}`,
		`"pidns":{"nsmode":"private"}`,
		`"ipcns":{"nsmode":"private"}`,
		`"userns":{"nsmode":"keep-id"}`,
		`"read_only_filesystem":true`,
		`"cap_drop":["ALL"]`,
		`"no_new_privileges":true`,
		`"user":"1001:1001"`,
		`"source":"/srv/work/repo","destination":"/workspace","type":"bind","options":["rw","rbind","rprivate"]`,
		`"source":"tmpfs","destination":"/tmp","type":"tmpfs","options":["rw","nosuid","nodev","size=268435456"]`,
		`"work_dir":"/workspace/sub"`,
		`"CI":"1"`,
		`"limit":805306368`,
		`"quota":75000`,
		`"period":100000`,
		`"pids":{"limit":128}`,
		`"log_configuration":{"driver":"k8s-file","size":1048576}`,
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("Podman create payload missing %q: %s", expected, text)
		}
	}
	for _, forbidden := range []string{"privileged", "docker.sock", "podman.sock", `"nsmode":"host"`, `"devices"`, `"cap_add"`} {
		if strings.Contains(text, forbidden) {
			t.Errorf("Podman create payload contains forbidden authority %q: %s", forbidden, text)
		}
	}
	if got := strings.Join(created.Command, "\x00"); got != strings.Join(spec.Argv, "\x00") {
		t.Fatalf("literal argv changed: %q", got)
	}
}

func TestPodmanEngineUsesBoundedAPIAndCleansUp(t *testing.T) {
	var paths []string
	engine := testPodmanEngine(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.Method+" "+request.URL.RequestURI())
		switch {
		case request.Method == http.MethodPost && request.URL.Path == podmanAPIRoot+"/containers/create":
			return apiResponse(http.StatusCreated, `{"Id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`), nil
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/start"):
			return apiResponse(http.StatusNoContent, ""), nil
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/wait"):
			return apiResponse(http.StatusOK, "7\n"), nil
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/logs"):
			return apiResponse(http.StatusOK, podmanLogFrame(1, strings.Repeat("o", 12))+podmanLogFrame(2, strings.Repeat("e", 12))), nil
		case request.Method == http.MethodDelete:
			return apiResponse(http.StatusNoContent, ""), nil
		default:
			return nil, errors.New("unexpected request: " + request.Method + " " + request.URL.String())
		}
	})
	response, err := engine.Run(context.Background(), RunSpec{
		WorkspaceRoot: t.TempDir(), Argv: []string{"true"}, NetworkProfile: "none",
		Timeout: time.Second, CPUMillis: 1000, MemoryMiB: 128, ProcessLimit: 16,
		OutputBytes: 16, Image: "image@" + executorTestDigest,
		IdempotencyKey: "sx_0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.ExitCode != 7 || !response.Truncated || len(response.Stdout)+len(response.Stderr) != 16 {
		t.Fatalf("bounded response is wrong: %#v", response)
	}
	if len(paths) != 5 || !strings.HasPrefix(paths[0], "POST "+podmanAPIRoot+"/containers/create") || !strings.HasPrefix(paths[4], "DELETE "+podmanAPIRoot+"/containers/") {
		t.Fatalf("unexpected API lifecycle: %#v", paths)
	}
}

func TestPodmanEngineAttestationRejectsRootlessOrImageDrift(t *testing.T) {
	for name, values := range map[string]struct {
		rootless bool
		digest   string
		socket   string
	}{
		"not rootless": {rootless: false, digest: executorTestDigest, socket: "unix:///run/user/1001/podman/podman.sock"},
		"image drift":  {rootless: true, digest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", socket: "unix:///run/user/1001/podman/podman.sock"},
		"socket drift": {rootless: true, digest: executorTestDigest, socket: "unix:///run/podman/podman.sock"},
	} {
		t.Run(name, func(t *testing.T) {
			engine := testPodmanEngine(func(request *http.Request) (*http.Response, error) {
				switch {
				case request.URL.Path == podmanAPIRoot+"/info":
					return apiResponse(http.StatusOK, `{"host":{"security":{"rootless":`+boolText(values.rootless)+`},"remoteSocket":{"path":"`+values.socket+`"}}}`), nil
				case strings.Contains(request.URL.Path, "/images/"):
					return apiResponse(http.StatusOK, `{"Digest":"`+values.digest+`"}`), nil
				default:
					return nil, errors.New("unexpected request")
				}
			})
			if err := engine.Attest(context.Background(), "image@"+executorTestDigest, executorTestDigest); err == nil {
				t.Fatal("drifting engine was accepted")
			}
		})
	}
}

func TestPodmanLogDecoderRejectsMalformedOrOversizedFrames(t *testing.T) {
	oversized := make([]byte, 8)
	oversized[0] = 1
	binary.BigEndian.PutUint32(oversized[4:], maxPodmanLogFrame+1)
	for name, encoded := range map[string]string{
		"unknown stream": podmanLogFrame(3, "x"),
		"reserved bytes": string([]byte{1, 1, 0, 0, 0, 0, 0, 0}),
		"oversized":      string(oversized),
		"truncated":      podmanLogFrame(1, "payload")[:10],
	} {
		t.Run(name, func(t *testing.T) {
			if err := copyPodmanLogFrames(strings.NewReader(encoded), newBoundedStreams(1024)); err == nil {
				t.Fatal("malformed Podman log stream was accepted")
			}
		})
	}
}

func TestPodmanAPIRejectsOversizedJSON(t *testing.T) {
	engine := testPodmanEngine(func(*http.Request) (*http.Response, error) {
		return apiResponse(http.StatusCreated, `{"Id":"`+strings.Repeat("a", maxPodmanResponseBody)+`"}`), nil
	})
	if _, err := engine.create(context.Background(), podmanCreateRequest{}); err == nil {
		t.Fatal("oversized Podman response was accepted")
	}
}

func TestPodmanEngineTimeoutStillForcesCleanup(t *testing.T) {
	cleaned := false
	engine := testPodmanEngine(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == podmanAPIRoot+"/containers/create":
			return apiResponse(http.StatusCreated, `{"Id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`), nil
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/start"):
			return apiResponse(http.StatusNoContent, ""), nil
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/wait"):
			<-request.Context().Done()
			return nil, request.Context().Err()
		case request.Method == http.MethodDelete:
			cleaned = true
			return apiResponse(http.StatusNoContent, ""), nil
		default:
			return nil, errors.New("unexpected request")
		}
	})
	_, err := engine.Run(context.Background(), RunSpec{
		WorkspaceRoot: t.TempDir(), Argv: []string{"sleep", "30"}, NetworkProfile: "none",
		Timeout: 10 * time.Millisecond, CPUMillis: 1000, MemoryMiB: 128, ProcessLimit: 16,
		OutputBytes: 16, Image: "image@" + executorTestDigest,
		IdempotencyKey: "sx_0123456789abcdef0123456789abcdef",
	})
	if err == nil || !strings.Contains(err.Error(), "timed out or was cancelled") || !cleaned {
		t.Fatalf("timeout did not fail closed with cleanup: cleaned=%v err=%v", cleaned, err)
	}
}

func TestPodmanEngineReconcilesAmbiguousCreateByDeterministicName(t *testing.T) {
	var deleted string
	engine := testPodmanEngine(func(request *http.Request) (*http.Response, error) {
		switch request.Method {
		case http.MethodPost:
			return nil, errors.New("lost create response")
		case http.MethodDelete:
			deleted = request.URL.Path
			return apiResponse(http.StatusNotFound, ""), nil
		default:
			return nil, errors.New("unexpected request")
		}
	})
	_, err := engine.Run(context.Background(), RunSpec{
		WorkspaceRoot: t.TempDir(), Argv: []string{"true"}, NetworkProfile: "none",
		Timeout: time.Second, CPUMillis: 1000, MemoryMiB: 128, ProcessLimit: 16,
		OutputBytes: 16, Image: "image@" + executorTestDigest,
		IdempotencyKey: "sx_0123456789abcdef0123456789abcdef",
	})
	if err == nil || !strings.Contains(err.Error(), "create failed") {
		t.Fatalf("ambiguous create did not fail closed: %v", err)
	}
	want := podmanAPIRoot + "/containers/aeontra-l3-0123456789abcdef0123456789abcdef"
	if deleted != want {
		t.Fatalf("ambiguous create cleanup path=%q want=%q", deleted, want)
	}
}

func TestPodmanEngineReportsExecutionAndCleanupFailures(t *testing.T) {
	engine := testPodmanEngine(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == podmanAPIRoot+"/containers/create":
			return apiResponse(http.StatusCreated, `{"Id":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}`), nil
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/start"):
			return apiResponse(http.StatusInternalServerError, "start failed"), nil
		case request.Method == http.MethodDelete:
			return apiResponse(http.StatusInternalServerError, "cleanup failed"), nil
		default:
			return nil, errors.New("unexpected request")
		}
	})
	_, err := engine.Run(context.Background(), RunSpec{
		WorkspaceRoot: t.TempDir(), Argv: []string{"true"}, NetworkProfile: "none",
		Timeout: time.Second, CPUMillis: 1000, MemoryMiB: 128, ProcessLimit: 16,
		OutputBytes: 16, Image: "image@" + executorTestDigest,
		IdempotencyKey: "sx_0123456789abcdef0123456789abcdef",
	})
	if err == nil || !strings.Contains(err.Error(), "execution failed") || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("combined lifecycle failure was not preserved: %v", err)
	}
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

var _ Engine = (*podmanEngine)(nil)
var _ = sandboxprotocol.Response{}
