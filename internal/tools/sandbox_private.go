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
	"net/http"
	"net/url"
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
	Client        *http.Client
}

type privateSandboxRunner struct {
	config PrivateSandboxConfig
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
	if config.Client == nil {
		config.Client = &http.Client{Timeout: config.Timeout + 10*time.Second}
	}
	runner := privateSandboxRunner{config: config}
	u, err := url.Parse(config.URL)
	switch {
	case err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "":
		runner.err = errors.New("private sandbox runner URL must be a credential-free HTTP endpoint")
	case len(config.Token) < 32:
		runner.err = errors.New("private sandbox runner token is missing or too short")
	case !privateSandboxWorkspacePattern.MatchString(config.WorkspaceID):
		runner.err = errors.New("private sandbox workspace identifier is invalid")
	case !filepath.IsAbs(config.WorkspaceRoot):
		runner.err = errors.New("private sandbox workspace root must be absolute")
	case !privateSandboxDigestPattern.MatchString(config.ImageDigest):
		runner.err = errors.New("private sandbox image digest must be sha256-pinned")
	}
	return runner
}

func (r privateSandboxRunner) Status(ctx context.Context) SandboxStatusInfo {
	status := SandboxStatusInfo{Backend: "private-rootless", DefaultEgress: "deny", FreeTerminal: false}
	if r.err != nil {
		status.Notes = []string{r.err.Error(), "no host execution fallback"}
		return status
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.config.URL+"/v1/status", nil)
	if err != nil {
		status.Notes = []string{"private executor status request is invalid", "no host execution fallback"}
		return status
	}
	request.Header.Set("Authorization", "Bearer "+r.config.Token)
	response, err := r.config.Client.Do(request)
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
	relative, err := filepath.Rel(r.config.WorkspaceRoot, filepath.Clean(input.Dir))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
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
		SchemaVersion: 1, IdempotencyKey: "sx_" + hex.EncodeToString(keyBytes),
		WorkspaceID: r.config.WorkspaceID, RelativeDir: filepath.ToSlash(relative),
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
	response, err := r.config.Client.Do(request)
	if err != nil {
		return SandboxRunResult{}, fmt.Errorf("calling private sandbox executor: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return SandboxRunResult{}, fmt.Errorf("private sandbox executor returned %s", response.Status)
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
