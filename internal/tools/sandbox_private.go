package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

const (
	privateSandboxBackend        = sandboxprotocol.Backend
	privateSandboxProfileVersion = sandboxprotocol.ProfileVersion
	privateSandboxMaxResponse    = 2 << 20
)

type privateSandboxStatus = sandboxprotocol.Status
type privateSandboxRequest = sandboxprotocol.Request
type privateSandboxResponse = sandboxprotocol.Response

var (
	privateSandboxWorkspacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	privateSandboxScopePattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	privateSandboxDigestPattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type PrivateSandboxConfig struct {
	URL           string
	Token         string
	WorkspaceID   string
	WorkspaceRoot string
	ImageDigest   string
	Timeout       time.Duration
	CPUMillis     int
	MemoryMiB     int
	ProcessLimit  int
	OutputBytes   int
}

type privateSandboxRunner struct {
	config PrivateSandboxConfig
	client *http.Client
	err    error
}

func NewPrivateSandboxRunner(config PrivateSandboxConfig) SandboxRunner {
	config.URL = strings.TrimRight(strings.TrimSpace(config.URL), "/")
	config.Token = strings.TrimSpace(config.Token)
	config.WorkspaceID = strings.TrimSpace(config.WorkspaceID)
	config.WorkspaceRoot = filepath.Clean(strings.TrimSpace(config.WorkspaceRoot))
	config.ImageDigest = strings.ToLower(strings.TrimSpace(config.ImageDigest))
	if config.Timeout <= 0 {
		config.Timeout = 2 * time.Minute
	}
	if config.CPUMillis <= 0 {
		config.CPUMillis = 1000
	}
	if config.MemoryMiB <= 0 {
		config.MemoryMiB = 1024
	}
	if config.ProcessLimit <= 0 {
		config.ProcessLimit = 256
	}
	if config.OutputBytes <= 0 {
		config.OutputBytes = 1 << 20
	}
	runner := privateSandboxRunner{config: config}
	u, err := url.Parse(config.URL)
	switch {
	case err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "":
		runner.err = errors.New("private sandbox runner URL must be a credential-free HTTP endpoint")
	case !privateSandboxURLHostAllowed(u.Hostname()):
		runner.err = errors.New("private sandbox runner URL must target a loopback or private endpoint")
	case len(config.Token) < 32:
		runner.err = errors.New("private sandbox runner token is missing or too short")
	case !privateSandboxWorkspacePattern.MatchString(config.WorkspaceID):
		runner.err = errors.New("private sandbox workspace identifier is invalid")
	case !filepath.IsAbs(config.WorkspaceRoot):
		runner.err = errors.New("private sandbox workspace root must be absolute")
	case !privateSandboxDigestPattern.MatchString(config.ImageDigest):
		runner.err = errors.New("private sandbox image digest must be sha256-pinned")
	}
	runner.client = newPrivateSandboxHTTPClient(config.Timeout + 10*time.Second)
	return runner
}

func privateSandboxURLHostAllowed(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate()
	}
	return !strings.ContainsAny(host, " /\\")
}

func newPrivateSandboxHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("private sandbox endpoint address is invalid")
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("private sandbox endpoint resolution failed")
		}
		for _, candidate := range addresses {
			if !candidate.IP.IsLoopback() && !candidate.IP.IsPrivate() {
				return nil, errors.New("private sandbox endpoint resolved outside private networks")
			}
		}
		var lastErr error
		for _, candidate := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if err == nil {
				return connection, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: rejectPrivateSandboxRedirect,
	}
}

func rejectPrivateSandboxRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

func (r privateSandboxRunner) Status(ctx context.Context) SandboxStatusInfo {
	status := SandboxStatusInfo{Backend: "private-rootless", DefaultEgress: "deny", FreeTerminal: false, NetworkPolicy: "unavailable", ToolchainState: "unavailable"}
	if r.err != nil {
		status.Notes = []string{r.err.Error(), "no host execution fallback"}
		return status
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.config.URL+"/v1/status?profile_version="+url.QueryEscape(sandboxprotocol.ProfileVersion), nil)
	if err != nil {
		status.Notes = []string{"private executor status request is invalid", "no host execution fallback"}
		return status
	}
	request.Header.Set("Authorization", "Bearer "+r.config.Token)
	response, err := r.client.Do(request)
	if err != nil {
		status.Notes = []string{"private executor is unreachable", "no host execution fallback"}
		return status
	}
	defer response.Body.Close()
	var remote sandboxprotocol.Status
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if response.StatusCode != http.StatusOK || decoder.Decode(&remote) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		!remote.Available || remote.Backend != sandboxprotocol.Backend || !remote.Rootless ||
		remote.NetworkProfile != "none" || remote.ImageDigest != r.config.ImageDigest ||
		remote.ProfileVersion != sandboxprotocol.ProfileVersion {
		status.Notes = []string{"private executor attestation is unavailable or does not match configuration", "no host execution fallback"}
		return status
	}
	status.Available = true
	status.FreeTerminal = true
	status.Backend = remote.Backend
	status.ContainerReady = true
	status.ExecReady = true
	status.FilesystemReady = true
	status.GitReady = true
	status.NetworkPolicy = "loaded:deny"
	status.ToolchainState = "core-ready"
	status.Notes = []string{
		"private rootless executor attested with a pinned image and network deny",
		"public MCP has no container-engine socket or host execution fallback",
	}
	return status
}

func (r privateSandboxRunner) Run(ctx context.Context, input SandboxRunRequest) (SandboxRunResult, error) {
	if r.err != nil {
		return SandboxRunResult{}, r.err
	}
	if len(input.Argv) == 0 {
		return SandboxRunResult{}, errors.New("sandbox: empty argv")
	}
	workspaceScope, relative, err := selectPrivateSandboxWorkspace(r.config.WorkspaceRoot, input.Dir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		if err != nil {
			return SandboxRunResult{}, err
		}
		return SandboxRunResult{}, errors.New("sandbox: working directory escapes configured workspace")
	}
	if relative == "." {
		relative = ""
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		return SandboxRunResult{}, fmt.Errorf("sandbox: creating idempotency identity: %w", err)
	}
	timeout := input.Timeout
	if timeout <= 0 || timeout > r.config.Timeout {
		timeout = r.config.Timeout
	}
	requestBody := sandboxprotocol.Request{
		SchemaVersion: 1, ProfileVersion: sandboxprotocol.ProfileVersion,
		IdempotencyKey: "sx_" + hex.EncodeToString(keyBytes),
		WorkspaceID:    r.config.WorkspaceID, WorkspaceScope: workspaceScope, RelativeDir: filepath.ToSlash(relative),
		Argv: append([]string(nil), input.Argv...), Environment: cloneSandboxEnvironment(input.EnvAllowlist),
		NetworkProfile: "none", TimeoutMS: timeout.Milliseconds(), CPUMillis: r.config.CPUMillis,
		MemoryMiB: r.config.MemoryMiB, ProcessLimit: r.config.ProcessLimit, OutputBytes: r.config.OutputBytes,
	}
	requestBody.RequestDigest, err = sandboxprotocol.Digest(requestBody)
	if err != nil {
		return SandboxRunResult{}, fmt.Errorf("sandbox: digesting request: %w", err)
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return SandboxRunResult{}, fmt.Errorf("sandbox: encoding request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.config.URL+"/v1/run", bytes.NewReader(encoded))
	if err != nil {
		return SandboxRunResult{}, err
	}
	request.Header.Set("Authorization", "Bearer "+r.config.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		return SandboxRunResult{}, fmt.Errorf("calling private sandbox executor: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return SandboxRunResult{}, decodePrivateSandboxError(response)
	}
	var result sandboxprotocol.Response
	decoder := json.NewDecoder(io.LimitReader(response.Body, privateSandboxMaxResponse))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return SandboxRunResult{}, errors.New("private sandbox executor returned an invalid result")
	}
	if result.IdempotencyKey != requestBody.IdempotencyKey || result.RequestDigest != requestBody.RequestDigest ||
		len(result.Stdout)+len(result.Stderr) > r.config.OutputBytes {
		return SandboxRunResult{}, errors.New("private sandbox executor returned an unbound or oversized result")
	}
	return SandboxRunResult{
		ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr,
		Duration:       time.Duration(result.DurationMS) * time.Millisecond,
		SandboxBackend: sandboxprotocol.Backend, EgressProfile: "none",
	}, nil
}

func selectPrivateSandboxWorkspace(root, dir string) (string, string, error) {
	root = filepath.Clean(root)
	dir = filepath.Clean(dir)
	relative, err := filepath.Rel(root, dir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", "", errors.New("sandbox: working directory escapes configured workspace")
	}
	if directSandboxRepository(root) {
		if relative == "." {
			relative = ""
		}
		return "", filepath.ToSlash(relative), nil
	}
	if relative == "." {
		return "", "", errors.New("sandbox: cwd must select a direct repository under the configured multi-repository root")
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) == 0 || !privateSandboxScopePattern.MatchString(parts[0]) {
		return "", "", errors.New("sandbox: selected workspace name is invalid")
	}
	selected, err := os.Lstat(filepath.Join(root, filepath.FromSlash(parts[0])))
	if err != nil || !selected.IsDir() || selected.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("sandbox: selected workspace is unavailable")
	}
	return parts[0], strings.Join(parts[1:], "/"), nil
}

func directSandboxRepository(root string) bool {
	gitMarker, err := os.Lstat(filepath.Join(root, ".git"))
	return err == nil && gitMarker.Mode()&os.ModeSymlink == 0 && (gitMarker.IsDir() || gitMarker.Mode().IsRegular())
}

func decodePrivateSandboxError(response *http.Response) error {
	limited := &io.LimitedReader{R: response.Body, N: 16<<10 + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	var remote sandboxprotocol.Error
	if err := decoder.Decode(&remote); err != nil || limited.N == 0 || decoder.Decode(&struct{}{}) != io.EOF ||
		!sandboxprotocol.ValidError(remote) {
		return fmt.Errorf("private sandbox executor returned HTTP %d with an invalid error response", response.StatusCode)
	}
	return fmt.Errorf("private sandbox executor rejected the request (%s): %s", remote.Code, remote.Message)
}

func cloneSandboxEnvironment(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
