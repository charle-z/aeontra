package sandboxexecutor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

const (
	podmanAPIRoot         = "/v5.0.0/libpod"
	maxPodmanResponseBody = 64 << 10
	maxPodmanLogFrame     = 8 << 20
	maxPodmanLogStream    = 16 << 20
)

var podmanContainerIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type podmanEngine struct {
	socket string
	uid    int
	gid    int
	client *http.Client
}

type podmanNamespace struct {
	NSMode string `json:"nsmode"`
}

type podmanMount struct {
	Source      string   `json:"source"`
	Destination string   `json:"destination"`
	Type        string   `json:"type"`
	Options     []string `json:"options"`
}

type podmanMemoryLimit struct {
	Limit int64 `json:"limit"`
}

type podmanCPULimit struct {
	Quota  int64  `json:"quota"`
	Period uint64 `json:"period"`
}

type podmanPIDsLimit struct {
	Limit int64 `json:"limit"`
}

type podmanResourceLimits struct {
	Memory *podmanMemoryLimit `json:"memory"`
	CPU    *podmanCPULimit    `json:"cpu"`
	PIDs   *podmanPIDsLimit   `json:"pids"`
}

type podmanLogConfiguration struct {
	Driver string `json:"driver"`
	Size   int64  `json:"size"`
}

// podmanCreateRequest is the deliberately small subset of Podman's SpecGenerator
// accepted by the private runner. Keeping this local avoids importing the full
// container-engine dependency graph into the security boundary.
type podmanCreateRequest struct {
	Name               string                 `json:"name"`
	Image              string                 `json:"image"`
	RawImageName       string                 `json:"raw_image_name"`
	Command            []string               `json:"command"`
	Env                map[string]string      `json:"env"`
	EnvHost            bool                   `json:"env_host"`
	HTTPProxy          bool                   `json:"httpproxy"`
	WorkDir            string                 `json:"work_dir"`
	User               string                 `json:"user"`
	UserNS             podmanNamespace        `json:"userns"`
	NetNS              podmanNamespace        `json:"netns"`
	PidNS              podmanNamespace        `json:"pidns"`
	IpcNS              podmanNamespace        `json:"ipcns"`
	ReadOnlyFilesystem bool                   `json:"read_only_filesystem"`
	NoNewPrivileges    bool                   `json:"no_new_privileges"`
	CapDrop            []string               `json:"cap_drop"`
	Mounts             []podmanMount          `json:"mounts"`
	ResourceLimits     podmanResourceLimits   `json:"resource_limits"`
	LogConfiguration   podmanLogConfiguration `json:"log_configuration"`
}

func (p *podmanEngine) Attest(ctx context.Context, image, digest string) error {
	if p.client == nil {
		return errors.New("podman API client is unavailable")
	}
	var info struct {
		Host struct {
			Security struct {
				Rootless bool `json:"rootless"`
			} `json:"security"`
			RemoteSocket struct {
				Path string `json:"path"`
			} `json:"remoteSocket"`
		} `json:"host"`
	}
	if err := p.decodeJSON(ctx, http.MethodGet, "/info", nil, nil, http.StatusOK, &info); err != nil ||
		!info.Host.Security.Rootless || info.Host.RemoteSocket.Path != "unix://"+p.socket {
		return errors.New("podman endpoint is not an attested rootless engine")
	}
	var inspected struct {
		Digest string `json:"Digest"`
	}
	imagePath := "/images/" + url.PathEscape(image) + "/json"
	if err := p.decodeJSON(ctx, http.MethodGet, imagePath, nil, nil, http.StatusOK, &inspected); err != nil || inspected.Digest != digest {
		return errors.New("sandbox image digest does not match the pinned identity")
	}
	return nil
}

func (p *podmanEngine) Ready(ctx context.Context, workspaceRoot, image string) error {
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		return errors.New("creating sandbox readiness identity")
	}
	response, err := p.Run(ctx, RunSpec{
		WorkspaceRoot:  workspaceRoot,
		Argv:           []string{"sh", "-c", "set -eu; test \"$(pwd)\" = /workspace; test -r /workspace; probe=$(mktemp /workspace/.aeontra-ready.XXXXXX); rm -f \"$probe\"; command -v git >/dev/null; command -v go >/dev/null; command -v cargo >/dev/null; command -v rustc >/dev/null; command -v node >/dev/null; command -v npm >/dev/null; command -v python3 >/dev/null; command -v cc >/dev/null; command -v c++ >/dev/null; printf ready"},
		NetworkProfile: "none", Timeout: 10 * time.Second, CPUMillis: 250,
		MemoryMiB: 128, ProcessLimit: 32, OutputBytes: 4096, Image: image,
		IdempotencyKey: "sx_" + hex.EncodeToString(keyBytes),
	})
	if err != nil || response.ExitCode != 0 || response.Stdout != "ready" || response.Stderr != "" || response.Truncated {
		return errors.New("sandbox readiness execution failed")
	}
	return nil
}

func (p *podmanEngine) Run(parent context.Context, spec RunSpec) (sandboxprotocol.Response, error) {
	if p.client == nil {
		return sandboxprotocol.Response{}, errors.New("podman API client is unavailable")
	}
	ctx, cancel := context.WithTimeout(parent, spec.Timeout)
	defer cancel()

	containerID, err := p.create(ctx, podmanCreateSpec(spec, p.uid, p.gid))
	if err != nil {
		cleanupErr := p.cleanup(podmanContainerName(spec.IdempotencyKey))
		if cleanupErr != nil {
			return sandboxprotocol.Response{}, errors.Join(
				fmt.Errorf("rootless Podman create failed: %w", err),
				fmt.Errorf("rootless Podman ambiguous-create cleanup failed: %w", cleanupErr),
			)
		}
		return sandboxprotocol.Response{}, fmt.Errorf("rootless Podman create failed: %w", err)
	}
	start := time.Now()
	var runErr error
	var exitCode int
	streams := newBoundedStreams(spec.OutputBytes)
	if err := p.noContent(ctx, http.MethodPost, "/containers/"+containerID+"/start", nil, http.StatusNoContent); err != nil {
		runErr = err
	} else if exitCode, err = p.wait(ctx, containerID); err != nil {
		runErr = err
	} else if err = p.streamLogs(ctx, containerID, streams); err != nil {
		runErr = err
	}
	duration := time.Since(start)

	cleanupErr := p.cleanup(containerID)
	if runErr != nil {
		if ctx.Err() != nil {
			runErr = fmt.Errorf("sandbox execution timed out or was cancelled: %w", ctx.Err())
		} else {
			runErr = fmt.Errorf("rootless Podman execution failed: %w", runErr)
		}
		if cleanupErr != nil {
			return sandboxprotocol.Response{}, errors.Join(runErr, fmt.Errorf("rootless Podman cleanup failed: %w", cleanupErr))
		}
		return sandboxprotocol.Response{}, runErr
	}
	if cleanupErr != nil {
		return sandboxprotocol.Response{}, fmt.Errorf("rootless Podman cleanup failed: %w", cleanupErr)
	}
	stdout, stderr, truncated := streams.result()
	return sandboxprotocol.Response{
		ExitCode: exitCode, Stdout: stdout, Stderr: stderr,
		DurationMS: duration.Milliseconds(), Truncated: truncated,
	}, nil
}

func (p *podmanEngine) cleanup(identity string) error {
	cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cleanupCancel()
	cleanupQuery := url.Values{"force": {"true"}}
	return p.noContent(
		cleanupContext,
		http.MethodDelete,
		"/containers/"+identity,
		cleanupQuery,
		http.StatusOK,
		http.StatusNoContent,
		http.StatusNotFound,
	)
}

func podmanContainerName(key string) string {
	return "aeontra-l3-" + strings.TrimPrefix(key, "sx_")
}

func podmanCreateSpec(spec RunSpec, uid, gid int) podmanCreateRequest {
	workdir := "/workspace"
	if spec.RelativeDir != "" && spec.RelativeDir != "." {
		workdir += "/" + strings.TrimPrefix(filepath.ToSlash(spec.RelativeDir), "/")
	}
	environment := map[string]string{
		"HOME": "/tmp/home", "TMPDIR": "/tmp",
		"XDG_CONFIG_HOME": "/tmp/config", "XDG_CACHE_HOME": "/tmp/cache",
	}
	for key, value := range spec.Environment {
		environment[key] = value
	}
	return podmanCreateRequest{
		Name: podmanContainerName(spec.IdempotencyKey), Image: spec.Image, RawImageName: spec.Image,
		Command: append([]string(nil), spec.Argv...), Env: environment, EnvHost: false, HTTPProxy: false,
		WorkDir: workdir, User: fmt.Sprintf("%d:%d", uid, gid),
		UserNS: podmanNamespace{NSMode: "keep-id"}, NetNS: podmanNamespace{NSMode: "none"},
		PidNS: podmanNamespace{NSMode: "private"}, IpcNS: podmanNamespace{NSMode: "private"},
		ReadOnlyFilesystem: true, NoNewPrivileges: true, CapDrop: []string{"ALL"},
		Mounts: []podmanMount{
			{Source: spec.WorkspaceRoot, Destination: "/workspace", Type: "bind", Options: []string{"rw", "rbind", "rprivate"}},
			{Source: "tmpfs", Destination: "/tmp", Type: "tmpfs", Options: []string{"rw", "exec", "nosuid", "nodev", "size=268435456"}},
		},
		ResourceLimits: podmanResourceLimits{
			Memory: &podmanMemoryLimit{Limit: int64(spec.MemoryMiB) << 20},
			CPU:    &podmanCPULimit{Quota: int64(spec.CPUMillis) * 100, Period: 100000},
			PIDs:   &podmanPIDsLimit{Limit: int64(spec.ProcessLimit)},
		},
		LogConfiguration: podmanLogConfiguration{Driver: "k8s-file", Size: max(int64(spec.OutputBytes), 1<<20)},
	}
}

func (p *podmanEngine) create(ctx context.Context, spec podmanCreateRequest) (string, error) {
	var created struct {
		ID string `json:"Id"`
	}
	if err := p.decodeJSON(ctx, http.MethodPost, "/containers/create", nil, spec, http.StatusCreated, &created); err != nil {
		return "", err
	}
	if !podmanContainerIDPattern.MatchString(created.ID) {
		return "", errors.New("podman returned an invalid container identity")
	}
	return created.ID, nil
}

func (p *podmanEngine) wait(ctx context.Context, containerID string) (int, error) {
	query := url.Values{"condition": {"exited"}, "interval": {"100ms"}}
	var exitCode int
	if err := p.decodeJSON(ctx, http.MethodPost, "/containers/"+containerID+"/wait", query, nil, http.StatusOK, &exitCode); err != nil {
		return 0, err
	}
	if exitCode < 0 || exitCode > 255 {
		return 0, errors.New("podman returned an invalid exit code")
	}
	return exitCode, nil
}

func (p *podmanEngine) streamLogs(ctx context.Context, containerID string, streams *boundedStreams) error {
	query := url.Values{
		"follow": {"false"}, "stdout": {strconv.FormatBool(true)}, "stderr": {strconv.FormatBool(true)},
		"timestamps": {"false"}, "tail": {"all"},
	}
	response, err := p.request(ctx, http.MethodGet, "/containers/"+containerID+"/logs", query, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxPodmanResponseBody))
		return fmt.Errorf("podman API logs returned status %d", response.StatusCode)
	}
	return copyPodmanLogFrames(response.Body, streams)
}

func copyPodmanLogFrames(reader io.Reader, streams *boundedStreams) error {
	var header [8]byte
	var total uint64
	for {
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return errors.New("podman log stream ended inside a frame header")
		}
		if header[1] != 0 || header[2] != 0 || header[3] != 0 {
			return errors.New("podman log stream header is invalid")
		}
		length := binary.BigEndian.Uint32(header[4:])
		if length > maxPodmanLogFrame {
			return errors.New("podman log frame exceeded its limit")
		}
		total += uint64(len(header)) + uint64(length)
		if total > maxPodmanLogStream {
			return errors.New("podman log stream exceeded its limit")
		}
		var writer io.Writer
		switch header[0] {
		case 1:
			writer = streams.stdout()
		case 2:
			writer = streams.stderr()
		default:
			return errors.New("podman log stream type is invalid")
		}
		if _, err := io.CopyN(writer, reader, int64(length)); err != nil {
			return errors.New("podman log stream ended inside a frame")
		}
	}
}

func (p *podmanEngine) noContent(ctx context.Context, method, endpoint string, query url.Values, expected ...int) error {
	response, err := p.request(ctx, method, endpoint, query, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if !statusAllowed(response.StatusCode, expected) {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxPodmanResponseBody))
		return fmt.Errorf("podman API returned status %d", response.StatusCode)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxPodmanResponseBody))
	return nil
}

func (p *podmanEngine) decodeJSON(ctx context.Context, method, endpoint string, query url.Values, input any, expected int, output any) error {
	response, err := p.request(ctx, method, endpoint, query, input)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != expected {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxPodmanResponseBody))
		return fmt.Errorf("podman API returned status %d", response.StatusCode)
	}
	limited := &io.LimitedReader{R: response.Body, N: maxPodmanResponseBody + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(output); err != nil {
		return errors.New("podman API returned invalid JSON")
	}
	if limited.N == 0 {
		return errors.New("podman API response exceeded its limit")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("podman API returned trailing data")
	}
	return nil
}

func (p *podmanEngine) request(ctx context.Context, method, endpoint string, query url.Values, input any) (*http.Response, error) {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return nil, errors.New("encoding Podman API request")
		}
		body = bytes.NewReader(encoded)
	}
	requestURL := "http://podman" + podmanAPIRoot + endpoint
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, errors.New("creating Podman API request")
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func statusAllowed(status int, allowed []int) bool {
	for _, candidate := range allowed {
		if status == candidate {
			return true
		}
	}
	return false
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
