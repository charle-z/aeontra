package sandboxexecutor

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

const maxWorkspaceEntries = 200000

var (
	idempotencyPattern = regexp.MustCompile(`^sx_[0-9a-f]{32}$`)
	digestPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	environmentPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
)

type Config struct {
	Token           string
	WorkspaceID     string
	WorkspaceRoot   string
	StateRoot       string
	Image           string
	ImageDigest     string
	MaxTimeoutMS    int64
	MaxCPUMillis    int
	MaxMemoryMiB    int
	MaxProcessLimit int
	MaxOutputBytes  int
	MaxConcurrent   int
	Engine          Engine
}

type RunSpec struct {
	WorkspaceRoot  string
	RelativeDir    string
	Argv           []string
	Environment    map[string]string
	NetworkProfile string
	Timeout        time.Duration
	CPUMillis      int
	MemoryMiB      int
	ProcessLimit   int
	OutputBytes    int
	Image          string
	IdempotencyKey string
}

type Engine interface {
	Attest(ctx context.Context, image, digest string) error
	Run(ctx context.Context, spec RunSpec) (sandboxprotocol.Response, error)
}

type Executor struct {
	config Config
	slots  chan struct{}
}

type receipt struct {
	SchemaVersion int                       `json:"schema_version"`
	Digest        string                    `json:"digest"`
	State         string                    `json:"state"`
	Response      *sandboxprotocol.Response `json:"response,omitempty"`
}

func New(config Config) (*Executor, error) {
	config.Token = strings.TrimSpace(config.Token)
	config.WorkspaceID = strings.TrimSpace(config.WorkspaceID)
	config.Image = strings.TrimSpace(config.Image)
	config.ImageDigest = strings.ToLower(strings.TrimSpace(config.ImageDigest))
	if config.Engine == nil {
		return nil, errors.New("sandbox executor engine is required")
	}
	if len(config.Token) < 32 {
		return nil, errors.New("sandbox executor token is missing or too short")
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`).MatchString(config.WorkspaceID) {
		return nil, errors.New("sandbox executor workspace identifier is invalid")
	}
	if !digestPattern.MatchString(config.ImageDigest) || !strings.HasSuffix(strings.ToLower(config.Image), "@"+config.ImageDigest) {
		return nil, errors.New("sandbox executor image must be pinned to the configured sha256 digest")
	}
	root, err := canonicalDirectory(config.WorkspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("sandbox executor workspace root: %w", err)
	}
	config.WorkspaceRoot = root
	if !filepath.IsAbs(config.StateRoot) {
		return nil, errors.New("sandbox executor state root must be absolute")
	}
	config.StateRoot = filepath.Clean(config.StateRoot)
	if err := os.MkdirAll(filepath.Join(config.StateRoot, "receipts"), 0o700); err != nil {
		return nil, fmt.Errorf("creating sandbox receipt root: %w", err)
	}
	stateRoot, err := canonicalDirectory(config.StateRoot)
	if err != nil {
		return nil, fmt.Errorf("sandbox executor state root: %w", err)
	}
	config.StateRoot = stateRoot
	if err := validatePrivateStateRoot(config.StateRoot); err != nil {
		return nil, fmt.Errorf("sandbox executor state root: %w", err)
	}
	if pathsOverlap(config.WorkspaceRoot, config.StateRoot) {
		return nil, errors.New("sandbox executor state root must not overlap the workspace")
	}
	if config.MaxTimeoutMS <= 0 || config.MaxTimeoutMS > int64((30*time.Minute)/time.Millisecond) ||
		config.MaxCPUMillis <= 0 || config.MaxMemoryMiB <= 0 || config.MaxProcessLimit <= 0 ||
		config.MaxOutputBytes <= 0 || config.MaxOutputBytes > 8<<20 {
		return nil, errors.New("sandbox executor resource maxima are invalid")
	}
	if config.MaxConcurrent <= 0 || config.MaxConcurrent > 64 {
		return nil, errors.New("sandbox executor concurrency limit is invalid")
	}
	return &Executor{config: config, slots: make(chan struct{}, config.MaxConcurrent)}, nil
}

func (e *Executor) Status(ctx context.Context) sandboxprotocol.Status {
	status := sandboxprotocol.Status{
		Backend: sandboxprotocol.Backend, NetworkProfile: "none",
		ImageDigest: e.config.ImageDigest, ProfileVersion: sandboxprotocol.ProfileVersion,
	}
	if err := e.config.Engine.Attest(ctx, e.config.Image, e.config.ImageDigest); err != nil {
		return status
	}
	status.Available = true
	status.Rootless = true
	return status
}

func (e *Executor) Execute(ctx context.Context, request sandboxprotocol.Request) (sandboxprotocol.Response, error) {
	if err := e.validateRequest(request); err != nil {
		return sandboxprotocol.Response{}, err
	}
	if completed, found, err := e.completedReceipt(request); err != nil {
		return sandboxprotocol.Response{}, err
	} else if found {
		return completed, nil
	}
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	case <-ctx.Done():
		return sandboxprotocol.Response{}, ctx.Err()
	}
	// A request with the same identity may have completed while this request
	// waited for capacity. Re-check the durable result after admission.
	if completed, found, err := e.completedReceipt(request); err != nil {
		return sandboxprotocol.Response{}, err
	} else if found {
		return completed, nil
	}
	rootBefore, err := os.Stat(e.config.WorkspaceRoot)
	if err != nil {
		return sandboxprotocol.Response{}, errors.New("sandbox workspace is unavailable")
	}
	relativeDir, err := validateRelativeDirectory(e.config.WorkspaceRoot, request.RelativeDir)
	if err != nil {
		return sandboxprotocol.Response{}, err
	}
	if err := scanWorkspace(e.config.WorkspaceRoot); err != nil {
		return sandboxprotocol.Response{}, err
	}
	rootAfter, err := os.Stat(e.config.WorkspaceRoot)
	if err != nil || !os.SameFile(rootBefore, rootAfter) {
		return sandboxprotocol.Response{}, errors.New("sandbox workspace identity changed during preflight")
	}
	if err := e.config.Engine.Attest(ctx, e.config.Image, e.config.ImageDigest); err != nil {
		return sandboxprotocol.Response{}, errors.New("sandbox executor attestation failed before execution")
	}
	runningPath, err := e.receiptPath(request.IdempotencyKey, ".running")
	if err != nil {
		return sandboxprotocol.Response{}, err
	}
	running := receipt{SchemaVersion: 1, Digest: request.RequestDigest, State: "running"}
	created, err := createExclusiveJSON(runningPath, running)
	if err != nil {
		return sandboxprotocol.Response{}, fmt.Errorf("creating sandbox receipt: %w", err)
	}
	if !created {
		existing, readErr := readReceipt(runningPath)
		if readErr != nil || existing.Digest != request.RequestDigest {
			return sandboxprotocol.Response{}, errors.New("sandbox idempotency identity conflicts with existing receipt")
		}
		return sandboxprotocol.Response{}, errors.New("sandbox execution outcome is indeterminate; refusing to repeat effect")
	}

	response, err := e.config.Engine.Run(ctx, RunSpec{
		WorkspaceRoot: e.config.WorkspaceRoot, RelativeDir: relativeDir,
		Argv: append([]string(nil), request.Argv...), Environment: cloneEnvironment(request.Environment),
		NetworkProfile: "none", Timeout: time.Duration(request.TimeoutMS) * time.Millisecond,
		CPUMillis: request.CPUMillis, MemoryMiB: request.MemoryMiB,
		ProcessLimit: request.ProcessLimit, OutputBytes: request.OutputBytes,
		Image: e.config.Image, IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		// The running receipt deliberately remains. A transport or engine error can be
		// ambiguous, so the same effect is never repeated under the same identity.
		return sandboxprotocol.Response{}, err
	}
	response.IdempotencyKey = request.IdempotencyKey
	response.RequestDigest = request.RequestDigest
	completed := receipt{SchemaVersion: 1, Digest: request.RequestDigest, State: "completed", Response: &response}
	completedPath, err := e.receiptPath(request.IdempotencyKey, ".done")
	if err != nil {
		return sandboxprotocol.Response{}, err
	}
	if err := writeAtomicJSON(completedPath, completed); err != nil {
		return sandboxprotocol.Response{}, fmt.Errorf("persisting sandbox completion receipt: %w", err)
	}
	// Keep the exclusive running marker. The completed receipt is authoritative,
	// while the marker prevents an already-completed identity from ever being
	// re-created across a concurrent stale preflight or a process restart.
	return response, nil
}

func (e *Executor) validateRequest(request sandboxprotocol.Request) error {
	if request.SchemaVersion != 1 || !idempotencyPattern.MatchString(request.IdempotencyKey) ||
		request.WorkspaceID != e.config.WorkspaceID || request.NetworkProfile != "none" {
		return errors.New("sandbox request authority is invalid")
	}
	digest, err := sandboxprotocol.Digest(request)
	if err != nil || subtle.ConstantTimeCompare([]byte(digest), []byte(request.RequestDigest)) != 1 {
		return errors.New("sandbox request digest is invalid")
	}
	if len(request.Argv) == 0 || len(request.Argv) > 128 {
		return errors.New("sandbox argv is invalid")
	}
	total := 0
	for _, argument := range request.Argv {
		total += len(argument)
		if argument == "" || strings.ContainsRune(argument, '\x00') || len(argument) > 32768 || total > 262144 {
			return errors.New("sandbox argv is invalid")
		}
	}
	if request.TimeoutMS <= 0 || request.TimeoutMS > e.config.MaxTimeoutMS ||
		request.CPUMillis <= 0 || request.CPUMillis > e.config.MaxCPUMillis ||
		request.MemoryMiB <= 0 || request.MemoryMiB > e.config.MaxMemoryMiB ||
		request.ProcessLimit <= 0 || request.ProcessLimit > e.config.MaxProcessLimit ||
		request.OutputBytes <= 0 || request.OutputBytes > e.config.MaxOutputBytes {
		return errors.New("sandbox request exceeds administrator resource limits")
	}
	if len(request.Environment) > 64 {
		return errors.New("sandbox environment is too large")
	}
	for key, value := range request.Environment {
		upper := strings.ToUpper(key)
		if !environmentPattern.MatchString(key) || len(value) > 4096 || strings.ContainsRune(value, '\x00') || sensitiveEnvironmentName(upper) {
			return errors.New("sandbox environment contains a forbidden entry")
		}
	}
	return nil
}

func sensitiveEnvironmentName(name string) bool {
	for _, fragment := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "COOKIE", "AUTHORIZATION"} {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	return name == "HOME" || name == "SSH_AUTH_SOCK" || name == "DOCKER_HOST" || name == "CONTAINER_HOST" || name == "GITHUB_TOKEN" || strings.HasSuffix(name, "_KEY")
}

func validateRelativeDirectory(root, raw string) (string, error) {
	if strings.Contains(raw, "\\") || strings.ContainsRune(raw, '\x00') || strings.Contains(raw, ":") || strings.HasPrefix(raw, "/") {
		return "", errors.New("sandbox relative working directory is invalid")
	}
	cleaned := path.Clean(raw)
	if cleaned == "." {
		cleaned = ""
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("sandbox relative working directory escapes workspace")
	}
	candidate := filepath.Join(root, filepath.FromSlash(cleaned))
	resolved, err := canonicalDirectory(candidate)
	if err != nil {
		return "", errors.New("sandbox working directory is unavailable")
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("sandbox working directory escapes workspace")
	}
	return filepath.ToSlash(relative), nil
}

func scanWorkspace(root string) error {
	entries := 0
	return filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("sandbox workspace preflight failed")
		}
		entries++
		if entries > maxWorkspaceEntries {
			return errors.New("sandbox workspace preflight exceeded entry limit")
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return errors.New("sandbox workspace preflight failed")
		}
		if relative != "." && policy.IsSecretPath(relative) {
			return errors.New("sandbox workspace contains a policy-denied secret path")
		}
		return nil
	})
}

func (e *Executor) receiptPath(key, suffix string) (string, error) {
	if !idempotencyPattern.MatchString(key) || (suffix != ".running" && suffix != ".done") {
		return "", errors.New("sandbox receipt identity is invalid")
	}
	name := key + suffix
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return "", errors.New("sandbox receipt identity is invalid")
	}
	receiptRoot, err := filepath.Abs(filepath.Join(e.config.StateRoot, "receipts"))
	if err != nil {
		return "", errors.New("sandbox receipt root is invalid")
	}
	candidate, err := filepath.Abs(filepath.Join(receiptRoot, name))
	if err != nil || !strings.HasPrefix(candidate, receiptRoot+string(filepath.Separator)) {
		return "", errors.New("sandbox receipt path escapes private state")
	}
	return candidate, nil
}

func (e *Executor) completedReceipt(request sandboxprotocol.Request) (sandboxprotocol.Response, bool, error) {
	completedPath, err := e.receiptPath(request.IdempotencyKey, ".done")
	if err != nil {
		return sandboxprotocol.Response{}, false, err
	}
	record, err := readReceipt(completedPath)
	if errors.Is(err, os.ErrNotExist) {
		return sandboxprotocol.Response{}, false, nil
	}
	if err != nil || record.SchemaVersion != 1 || record.State != "completed" || record.Response == nil || record.Digest != request.RequestDigest ||
		record.Response.IdempotencyKey != request.IdempotencyKey || record.Response.RequestDigest != request.RequestDigest {
		return sandboxprotocol.Response{}, false, errors.New("sandbox completion receipt is invalid or conflicts with request")
	}
	return *record.Response, true, nil
}

func createExclusiveJSON(file string, value any) (bool, error) {
	handle, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	encoderErr := json.NewEncoder(handle).Encode(value)
	syncErr := handle.Sync()
	closeErr := handle.Close()
	if encoderErr != nil {
		return false, encoderErr
	}
	if syncErr != nil {
		return false, syncErr
	}
	return true, closeErr
}

func writeAtomicJSON(file string, value any) error {
	temporary, err := os.CreateTemp(filepath.Dir(file), ".receipt-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := json.NewEncoder(temporary).Encode(value); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, file)
}

func readReceipt(file string) (receipt, error) {
	handle, err := os.Open(file)
	if err != nil {
		return receipt{}, err
	}
	defer handle.Close()
	var value receipt
	decoder := json.NewDecoder(handle)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return receipt{}, errors.New("invalid sandbox receipt")
	}
	return value, nil
}

func cloneEnvironment(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func pathsOverlap(left, right string) bool {
	relative, err := filepath.Rel(left, right)
	if err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))) {
		return true
	}
	relative, err = filepath.Rel(right, left)
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))
}
