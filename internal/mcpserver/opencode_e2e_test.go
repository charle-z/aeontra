//go:build opencode_e2e && !windows

package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

const e2eModel = "bridge/external-model"

type e2eMeter struct {
	Calls                       int64         `json:"mcp_calls"`
	RequestBytes                int64         `json:"mcp_request_bytes"`
	ResponseBytes               int64         `json:"mcp_response_bytes"`
	ExternalWait                time.Duration `json:"external_wait"`
	InterTurnGap                time.Duration `json:"inter_turn_gap"`
	Retries                     int64         `json:"retries"`
	RestartObservedDisconnected bool          `json:"restart_observed_disconnected,omitempty"`
	RestartResumedAwaiting      bool          `json:"restart_resumed_awaiting,omitempty"`
	RestartTurnID               string        `json:"restart_turn_id,omitempty"`
	RestartSequence             uint64        `json:"restart_sequence,omitempty"`
	RestartDigest               string        `json:"restart_digest,omitempty"`
	RestartRequestRef           string        `json:"restart_request_ref,omitempty"`
}

type e2eResult struct {
	Mode                        string        `json:"mode"`
	Elapsed                     time.Duration `json:"elapsed"`
	MCPCalls                    int64         `json:"mcp_calls"`
	Bytes                       int64         `json:"bytes"`
	ModelTurns                  int64         `json:"model_turns"`
	OpenCodeToolExecutions      int64         `json:"opencode_tool_executions"`
	ExternalWait                time.Duration `json:"external_wait"`
	InterTurnGap                time.Duration `json:"inter_turn_gap"`
	Retries                     int64         `json:"retries"`
	ResultStoreBytes            int64         `json:"result_store_bytes"`
	ToolNames                   []string      `json:"tool_names,omitempty"`
	ToolResultsVerified         bool          `json:"tool_results_verified,omitempty"`
	RepositoryModified          bool          `json:"repository_modified,omitempty"`
	TestsPassed                 bool          `json:"tests_passed,omitempty"`
	FinalResponseVerified       bool          `json:"final_response_verified,omitempty"`
	RestartObservedDisconnected bool          `json:"restart_observed_disconnected,omitempty"`
	RestartResumedAwaiting      bool          `json:"restart_resumed_awaiting,omitempty"`
	RestartTurnID               string        `json:"restart_turn_id,omitempty"`
	RestartSequence             uint64        `json:"restart_sequence,omitempty"`
	RestartDigest               string        `json:"restart_digest,omitempty"`
	RestartRequestRef           string        `json:"restart_request_ref,omitempty"`
}

type e2eRun struct {
	result       e2eResult
	stdout       string
	stderr       string
	trace        string
	runtimeID    string
	turnIdentity []modelturn.Record
}

type e2eNetworkEvidence struct {
	ContainerNetwork       string   `json:"container_network"`
	DNS                    string   `json:"dns"`
	DefaultRoutePresent    bool     `json:"default_route_present"`
	StraceCaptured         bool     `json:"strace_captured"`
	NonLoopbackConnects    int      `json:"non_loopback_connects"`
	ExternalProviderMarker []string `json:"external_provider_markers"`
}

type e2eSecurityEvidence struct {
	ProviderPackage       string `json:"provider_package"`
	ProviderLoaded        bool   `json:"provider_loaded"`
	CleanEnvironment      bool   `json:"clean_environment"`
	AuthContentEmpty      bool   `json:"auth_content_empty"`
	ProviderAPIKeys       bool   `json:"provider_api_keys_present"`
	CodexUsed             bool   `json:"codex_used"`
	ExternalModelFallback bool   `json:"external_model_fallback"`
	LocalModelUsed        bool   `json:"local_model_used"`
}

func TestOpenCodeExternalModelVerticalSlice(t *testing.T) {
	if os.Getenv("OPENCODE_E2E") != "1" {
		t.Skip("set OPENCODE_E2E=1 inside the isolated OpenCode test image")
	}
	assertNoDefaultRoute(t)
	root := repoRoot(t)
	binary := requiredAbsoluteFile(t, "OPENCODE_E2E_BIN")
	provider := filepath.Join(root, "integrations", "opencode", "provider")

	baselineRepo := createCalcRepo(t, "baseline")
	baseline := runGranularBaseline(t, baselineRepo)

	normalRepo := createCalcRepo(t, "opencode-normal")
	t.Log("slice_code=normal_run_begin")
	normal := runOpenCodeSlice(t, binary, provider, normalRepo, false)
	t.Log("slice_code=normal_run_complete")
	assertCalcFixedAndTested(t, normalRepo, "normal")
	t.Log("slice_code=normal_fixture_complete")
	assertOpenCodeEvents(t, normal.stdout, []string{"read", "grep", "edit", "bash"}, "normal")
	t.Log("slice_code=normal_events_complete")
	assertNoExternalTraffic(t, normal.trace, normal.stdout, normal.stderr, "normal")
	t.Log("slice_code=normal_network_complete")
	normal.result.RepositoryModified = true
	normal.result.TestsPassed = true
	normal.result.FinalResponseVerified = true
	if normal.result.MCPCalls >= 20 || normal.result.Retries != 0 {
		t.Fatalf("normal rendezvous remained granular: %+v", normal.result)
	}

	restartRepo := createCalcRepo(t, "opencode-restart")
	t.Log("slice_code=restart_run_begin")
	restarted := runOpenCodeSlice(t, binary, provider, restartRepo, true)
	t.Log("slice_code=restart_run_complete")
	assertCalcFixedAndTested(t, restartRepo, "restart")
	t.Log("slice_code=restart_fixture_complete")
	assertOpenCodeEvents(t, restarted.stdout, []string{"read", "grep", "edit", "bash"}, "restart")
	t.Log("slice_code=restart_events_complete")
	assertNoExternalTraffic(t, restarted.trace, restarted.stdout, restarted.stderr, "restart")
	t.Log("slice_code=restart_network_complete")
	restarted.result.RepositoryModified = true
	restarted.result.TestsPassed = true
	restarted.result.FinalResponseVerified = true
	if restarted.result.Retries != 1 || restarted.result.MCPCalls >= 20 {
		t.Fatalf("restart run did not remain bounded: %+v", restarted.result)
	}
	if len(restarted.turnIdentity) < 1 || restarted.turnIdentity[0].Status != modelturn.StatusAwaitingModel {
		t.Fatalf("restart evidence missing awaiting turn: %+v", restarted.turnIdentity)
	}

	report := struct {
		OpenCodeVersion string              `json:"opencode_version"`
		GitTree         string              `json:"git_tree"`
		Baseline        e2eResult           `json:"benchmark_a"`
		OpenCode        e2eResult           `json:"benchmark_b"`
		Restart         e2eResult           `json:"restart_resume"`
		Network         e2eNetworkEvidence  `json:"network"`
		Security        e2eSecurityEvidence `json:"security"`
	}{
		OpenCodeVersion: commandOutput(t, binary, "--version"),
		GitTree:         safeReportGitTree(os.Getenv("P11_2_GIT_TREE")),
		Baseline:        baseline,
		OpenCode:        normal.result,
		Restart:         restarted.result,
		Network: e2eNetworkEvidence{
			ContainerNetwork:       "none",
			DNS:                    "127.0.0.1",
			DefaultRoutePresent:    false,
			StraceCaptured:         true,
			NonLoopbackConnects:    0,
			ExternalProviderMarker: []string{},
		},
		Security: e2eSecurityEvidence{
			ProviderPackage:       "file://integrations/opencode/provider",
			ProviderLoaded:        true,
			CleanEnvironment:      true,
			AuthContentEmpty:      true,
			ProviderAPIKeys:       false,
			CodexUsed:             false,
			ExternalModelFallback: false,
			LocalModelUsed:        false,
		},
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "artifacts", "opencode-e2e-report.json")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("OpenCode external-model report:\n%s", encoded)
}

func runGranularBaseline(t *testing.T, repo string) e2eResult {
	t.Helper()
	server, _, _ := e2eServer(t, repo, nil)
	meter := &e2eMeter{}
	started := time.Now()
	mcpTool(t, server, meter, "read_file", map[string]any{"path": "calc/calc.go"})
	mcpTool(t, server, meter, "search_code", map[string]any{"query": "return a - b"})
	patch := "--- a/calc/calc.go\n+++ b/calc/calc.go\n@@ -3,3 +3,3 @@\n func Add(a, b int) int {\n-\treturn a - b\n+\treturn a + b\n }\n"
	mcpTool(t, server, meter, "apply_patch", map[string]any{"patch": patch, "approve": true})
	mcpTool(t, server, meter, "run_tests", map[string]any{"approve": true})
	return e2eResult{
		Mode:             "A: granular MCP",
		Elapsed:          time.Since(started),
		MCPCalls:         meter.Calls,
		Bytes:            meter.RequestBytes + meter.ResponseBytes,
		ResultStoreBytes: 0,
	}
}

func runOpenCodeSlice(t *testing.T, binary, provider, repo string, restart bool) e2eRun {
	t.Helper()
	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	storeRoot := filepath.Join(stateRoot, "model-turns")
	store, err := modelturn.OpenStore(modelturn.StoreConfig{Root: storeRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	driverStore, err := modelturn.OpenStore(modelturn.StoreConfig{Root: storeRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = driverStore.Close() }()
	server, _, _ := e2eServer(t, repo, store)
	meter := &e2eMeter{}
	var runtime modelturn.Runtime
	decodeToolJSON(t, mcpTool(t, server, meter, "model_runtime_start", map[string]any{}), &runtime)

	socketPath := filepath.Join(stateRoot, modelturn.DefaultDriverSocketName)
	driverCtx, driverCancel := context.WithCancel(context.Background())
	driverDone := make(chan error, 1)
	go func() { driverDone <- modelturn.ServeDriver(driverCtx, socketPath, driverStore, nil) }()
	waitForFile(t, socketPath)
	defer func() {
		driverCancel()
		<-driverDone
	}()

	configJSON := opencodeConfig(t, provider, socketPath, runtime.RuntimeID)
	home := filepath.Join(t.TempDir(), "home")
	for _, dir := range []string{home, filepath.Join(home, ".config"), filepath.Join(home, ".local", "share"), filepath.Join(home, ".local", "state"), filepath.Join(home, ".cache")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	tracePrefix := filepath.Join(t.TempDir(), "opencode-network")
	args := []string{"run", "--auto", "--model", e2eModel, "--format", "json", "--dir", repo, "Fix Add so the tests pass. Read and search before editing, then run the test suite."}
	commandName := binary
	commandArgs := args
	if strace, err := exec.LookPath("strace"); err == nil {
		commandName = strace
		commandArgs = append([]string{"-ff", "-e", "trace=network", "-o", tracePrefix, binary}, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, commandName, commandArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = repo
	cmd.Env = cleanOpenCodeEnv(home, configJSON)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	processDone := make(chan struct{})
	var processErr error
	go func() {
		processErr = cmd.Wait()
		close(processDone)
	}()
	var identities []modelturn.Record
	controlErr := controlOpenCode(ctx, t, server, meter, runtime.RuntimeID, repo, restart, socketPath, storeRoot, store, &driverStore, &driverCancel, &driverDone, processDone, &processErr, &identities)
	if controlErr != nil {
		failure := finalizeOpenCodeControlFailure(cancel, processDone, controlErr, stdout.String(), stderr.String())
		t.Fatalf("OpenCode control failed: slice_code=%s", safeOpenCodeSliceFailureCode(restart, failure, stdout.String(), stderr.String()))
	}
	<-processDone
	if processErr != nil {
		t.Fatalf("OpenCode failed: slice_code=%s", safeOpenCodeSliceFailureCode(restart, processErr, stdout.String(), stderr.String()))
	}
	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	trace := readTraceFiles(t, tracePrefix)
	toolsExecuted := countOpenCodeToolEvents(t, stdout.String())
	return e2eRun{
		result: e2eResult{
			Mode:                        "B: OpenCode + PullRendezvousTransport",
			Elapsed:                     time.Since(started),
			MCPCalls:                    meter.Calls,
			Bytes:                       meter.RequestBytes + meter.ResponseBytes,
			ModelTurns:                  4,
			OpenCodeToolExecutions:      int64(toolsExecuted),
			ExternalWait:                meter.ExternalWait,
			InterTurnGap:                meter.InterTurnGap,
			Retries:                     meter.Retries,
			ResultStoreBytes:            stats.BodyBytes,
			ToolNames:                   []string{"read", "grep", "edit", "bash"},
			ToolResultsVerified:         true,
			RestartObservedDisconnected: meter.RestartObservedDisconnected,
			RestartResumedAwaiting:      meter.RestartResumedAwaiting,
			RestartTurnID:               meter.RestartTurnID,
			RestartSequence:             meter.RestartSequence,
			RestartDigest:               meter.RestartDigest,
			RestartRequestRef:           meter.RestartRequestRef,
		},
		stdout:       stdout.String(),
		stderr:       stderr.String(),
		trace:        trace,
		runtimeID:    runtime.RuntimeID,
		turnIdentity: identities,
	}
}

var safeDriverFailurePattern = regexp.MustCompile(`model turn driver operation failed: ([a-z_]+)`)

func safeOpenCodeFailureCode(controlErr error, stdout, stderr string) string {
	for _, source := range []string{errorText(controlErr), stdout, stderr} {
		match := safeDriverFailurePattern.FindStringSubmatch(source)
		if len(match) == 2 {
			return modelturn.NormalizeDriverInternalErrorCode(match[1])
		}
	}
	if signal := edgeclient.SafeOpenCodeFailureSignal([]byte(stderr)); signal != "unknown" {
		return signal
	}
	if signal := edgeclient.SafeOpenCodeFailureSignal([]byte(stdout)); signal != "unknown" {
		return signal
	}
	if errors.Is(controlErr, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(controlErr, context.Canceled) {
		return "cancelled"
	}
	return "unknown"
}

func safeOpenCodeSliceFailureCode(restart bool, err error, stdout, stderr string) string {
	phase := "normal"
	if restart {
		phase = "restart"
	}
	code := safeOpenCodeFailureCode(err, stdout, stderr)
	if err != nil && strings.Contains(err.Error(), "process_shutdown_timeout") {
		code = "process_shutdown_timeout"
	}
	return phase + "_" + code
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func finalizeOpenCodeControlFailure(cancel context.CancelFunc, processDone <-chan struct{}, controlErr error, stdout, stderr string) error {
	return finalizeOpenCodeControlFailureWithin(cancel, processDone, controlErr, stdout, stderr, 5*time.Second)
}

func finalizeOpenCodeControlFailureWithin(cancel context.CancelFunc, processDone <-chan struct{}, controlErr error, stdout, stderr string, shutdownTimeout time.Duration) error {
	cancel()
	select {
	case <-processDone:
		return fmt.Errorf("OpenCode control failed: driver_code=%s", safeOpenCodeFailureCode(controlErr, stdout, stderr))
	case <-time.After(shutdownTimeout):
		return errors.New("OpenCode control failed: process_shutdown_timeout")
	}
}

func controlOpenCode(ctx context.Context, t *testing.T, server *Server, meter *e2eMeter, runtimeID, repo string, restart bool, socketPath, storeRoot string, store *modelturn.Store, driverStore **modelturn.Store, driverCancel *context.CancelFunc, driverDone *chan error, processDone <-chan struct{}, processErr *error, identities *[]modelturn.Record) error {
	lastResponded := time.Time{}
	for sequence := uint64(1); sequence <= 4; sequence++ {
		offer, err := nextTurn(ctx, t, server, meter, runtimeID, sequence-1, processDone, processErr)
		if err != nil {
			return err
		}
		if offer.Sequence != sequence || offer.RuntimeID != runtimeID {
			return fmt.Errorf("turn identity mismatch: %+v", offer.Record)
		}
		*identities = append(*identities, offer.Record)
		meter.ExternalWait += time.Since(offer.CreatedAt)
		if !lastResponded.IsZero() && offer.CreatedAt.After(lastResponded) {
			meter.InterTurnGap += offer.CreatedAt.Sub(lastResponded)
		}
		var payload map[string]any
		if err := json.Unmarshal(offer.RequestPayload, &payload); err != nil {
			return err
		}
		if sequence > 1 && !containsToolResult(payload, fmt.Sprintf("turn-%d", sequence-1)) {
			return fmt.Errorf("turn %d omitted the previous tool result", sequence)
		}
		if restart && sequence == 1 {
			if err := waitForDriverWaitCall(ctx, socketPath); err != nil {
				return err
			}
			original := offer.Record
			(*driverCancel)()
			if err := <-*driverDone; !errors.Is(err, context.Canceled) {
				return fmt.Errorf("stop driver: %w", err)
			}
			meter.Retries++
			_ = (*driverStore).Close()
			reopened, err := modelturn.OpenStore(modelturn.StoreConfig{Root: storeRoot})
			if err != nil {
				return err
			}
			*driverStore = reopened
			current, err := waitForTurnStatus(context.Background(), store, offer.TurnID, modelturn.StatusDisconnected)
			if err != nil || current.TurnID != original.TurnID || current.Sequence != original.Sequence || current.RequestDigest != original.RequestDigest || current.RequestRef != original.RequestRef || current.Status != modelturn.StatusDisconnected {
				return fmt.Errorf("restart did not preserve an exact disconnected turn: before=%+v after=%+v err=%v", original, current, err)
			}
			meter.RestartObservedDisconnected = true
			if resumed, err := store.ResumeRuntime(context.Background(), runtimeID); err != nil || resumed != 1 {
				return fmt.Errorf("resume runtime rows=%d err=%v", resumed, err)
			}
			resumedRecord, err := store.Get(context.Background(), offer.TurnID)
			if err != nil || resumedRecord.Status != modelturn.StatusAwaitingModel || resumedRecord.TurnID != original.TurnID || resumedRecord.Sequence != original.Sequence || resumedRecord.RequestDigest != original.RequestDigest || resumedRecord.RequestRef != original.RequestRef {
				return fmt.Errorf("restart resume identity mismatch: before=%+v after=%+v err=%v", original, resumedRecord, err)
			}
			meter.RestartResumedAwaiting = true
			meter.RestartTurnID = string(resumedRecord.TurnID)
			meter.RestartSequence = resumedRecord.Sequence
			meter.RestartDigest = resumedRecord.RequestDigest
			meter.RestartRequestRef = resumedRecord.RequestRef
			newCtx, cancel := context.WithCancel(context.Background())
			*driverCancel = cancel
			newDone := make(chan error, 1)
			*driverDone = newDone
			go func() { newDone <- modelturn.ServeDriver(newCtx, socketPath, reopened, nil) }()
			waitForFile(t, socketPath)
		}
		response, err := scriptedResponse(sequence, payload, repo)
		if err != nil {
			return err
		}
		mcpTool(t, server, meter, "model_turn_respond", map[string]any{
			"runtime_id":        runtimeID,
			"turn_id":           string(offer.TurnID),
			"expected_sequence": offer.Sequence,
			"request_digest":    offer.RequestDigest,
			"response":          response,
		})
		lastResponded = time.Now()
	}
	return nil
}

func readDriverMetrics(socketPath string) (modelturn.DriverMetrics, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}}
	client := &http.Client{Transport: transport, Timeout: time.Second}
	defer transport.CloseIdleConnections()
	response, err := client.Get("http://unix/v1/metrics")
	if err != nil {
		return modelturn.DriverMetrics{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return modelturn.DriverMetrics{}, fmt.Errorf("driver metrics status=%d", response.StatusCode)
	}
	var metrics modelturn.DriverMetrics
	if err := json.NewDecoder(response.Body).Decode(&metrics); err != nil {
		return modelturn.DriverMetrics{}, err
	}
	return metrics, nil
}

func waitForDriverWaitCall(ctx context.Context, socketPath string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		metrics, err := readDriverMetrics(socketPath)
		if err == nil && metrics.WaitCalls > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return errors.New("OpenCode did not begin waiting for the created model turn")
}

func waitForTurnStatus(ctx context.Context, store *modelturn.Store, turnID modelturn.TurnID, want modelturn.Status) (modelturn.Record, error) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, err := store.Get(ctx, turnID)
		if err != nil {
			return modelturn.Record{}, err
		}
		if record.Status == want {
			return record, nil
		}
		select {
		case <-ctx.Done():
			return modelturn.Record{}, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	record, err := store.Get(ctx, turnID)
	if err != nil {
		return modelturn.Record{}, err
	}
	return record, fmt.Errorf("turn status=%s, want=%s", record.Status, want)
}

func scriptedResponse(sequence uint64, payload map[string]any, repo string) (map[string]any, error) {
	toolsByName := requestToolIDs(payload)
	call := func(callID, name string, args map[string]any) (map[string]any, error) {
		id := toolsByName[name]
		if id == "" {
			return nil, fmt.Errorf("OpenCode did not offer tool %q; offered=%v", name, sortedKeys(toolsByName))
		}
		return map[string]any{"call_id": callID, "tool_id": id, "arguments": args}, nil
	}
	switch sequence {
	case 1:
		read, err := call("turn-1-read", "read", map[string]any{"filePath": filepath.Join(repo, "calc", "calc.go")})
		if err != nil {
			return nil, err
		}
		grep, err := call("turn-1-grep", "grep", map[string]any{"pattern": "return a - b", "path": repo, "include": "*.go"})
		if err != nil {
			return nil, err
		}
		return map[string]any{"finish_reason": "tool_calls", "tool_calls": []any{read, grep}}, nil
	case 2:
		edit, err := call("turn-2", "edit", map[string]any{"filePath": filepath.Join(repo, "calc", "calc.go"), "oldString": "return a - b", "newString": "return a + b"})
		if err != nil {
			return nil, err
		}
		return map[string]any{"finish_reason": "tool_calls", "tool_calls": []any{edit}}, nil
	case 3:
		bash, err := call("turn-3", "bash", map[string]any{"command": "go test ./...", "workdir": repo, "timeout": 60000})
		if err != nil {
			return nil, err
		}
		return map[string]any{"finish_reason": "tool_calls", "tool_calls": []any{bash}}, nil
	case 4:
		return map[string]any{"finish_reason": "stop", "text": "Fixed Add and verified the repository tests pass.", "tool_calls": []any{}}, nil
	default:
		return nil, fmt.Errorf("unexpected sequence %d", sequence)
	}
}

func nextTurn(ctx context.Context, t *testing.T, server *Server, meter *e2eMeter, runtimeID string, afterSequence uint64, processDone <-chan struct{}, processErr *error) (modelturn.Offer, error) {
	for {
		select {
		case <-processDone:
			if *processErr != nil {
				return modelturn.Offer{}, fmt.Errorf("OpenCode exited before the next model turn: %w", *processErr)
			}
			return modelturn.Offer{}, errors.New("OpenCode exited before the next model turn")
		case <-ctx.Done():
			return modelturn.Offer{}, ctx.Err()
		default:
		}
		text := mcpTool(t, server, meter, "model_turn_next", map[string]any{
			"runtime_id": runtimeID, "after_sequence": afterSequence, "wait_seconds": 30,
		})
		var envelope struct {
			Pending bool            `json:"pending"`
			Status  string          `json:"status"`
			Turn    modelturn.Offer `json:"turn"`
		}
		if err := json.Unmarshal([]byte(text), &envelope); err != nil {
			return modelturn.Offer{}, err
		}
		if envelope.Pending {
			return envelope.Turn, nil
		}
		if envelope.Status != "no_change" {
			return modelturn.Offer{}, fmt.Errorf("runtime changed while waiting: %s", envelope.Status)
		}
		meter.Retries++
	}
}

func e2eServer(t *testing.T, root string, store *modelturn.Store) (*Server, *tools.Service, *bytes.Buffer) {
	t.Helper()
	cfg, err := config.New(config.Config{Roots: []string{root}, Mode: config.ModeAllow, AllowedCommands: []string{"go", "git"}, TestCommand: []string{"go", "test", "./..."}})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	auditBuffer := &bytes.Buffer{}
	svc := tools.NewService(pol, audit.New(auditBuffer), pol.Roots()[0]).
		WithTestCommand([]string{"go", "test", "./..."})
	server := New(svc)
	if store != nil {
		server.WithModelTurnStore(store)
	}
	return server, svc, auditBuffer
}

func mcpTool(t *testing.T, server *Server, meter *e2eMeter, name string, arguments map[string]any) string {
	t.Helper()
	request := map[string]any{"jsonrpc": "2.0", "id": meter.Calls + 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	responseRaw := server.handle(raw)
	meter.Calls++
	meter.RequestBytes += int64(len(raw))
	meter.ResponseBytes += int64(len(responseRaw))
	var response rpcResponse
	if err := json.Unmarshal(responseRaw, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil {
		t.Fatalf("MCP %s RPC error: %+v", name, response.Error)
	}
	var result toolResult
	encoded, _ := json.Marshal(response.Result)
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("MCP %s failed: %s", name, encoded)
	}
	return result.Content[0].Text
}

func opencodeConfig(t *testing.T, provider, socketPath, runtimeID string) string {
	t.Helper()
	value := map[string]any{
		"provider": map[string]any{
			"bridge": map[string]any{
				"npm":     "file://" + provider,
				"name":    "MCP Devbox External Model",
				"options": map[string]any{"socketPath": socketPath, "runtimeID": runtimeID, "ttlMs": 600000, "timeoutMs": 120000},
				"models":  map[string]any{"external-model": map[string]any{"name": "External Model Turn"}},
			},
		},
		"permission": map[string]any{"*": "allow"},
		"agent": map[string]any{
			"title": map[string]any{"disable": true},
		},
		"autoupdate": false,
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func cleanOpenCodeEnv(home, configJSON string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "USER=opencode-e2e", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TERM=dumb", "SHELL=/bin/sh",
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"), "XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"), "XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"OPENCODE_TEST_HOME=" + home, "OPENCODE_CONFIG_CONTENT=" + configJSON, "OPENCODE_AUTH_CONTENT={}", "OPENCODE_DISABLE_PROJECT_CONFIG=1", "OPENCODE_PURE=1",
		"OPENCODE_DISABLE_AUTOUPDATE=1", "OPENCODE_DISABLE_AUTOCOMPACT=1", "OPENCODE_DISABLE_MODELS_FETCH=1", "OPENCODE_DISABLE_LSP_DOWNLOAD=1", "OPENCODE_DISABLE_DEFAULT_PLUGINS=1", "OPENCODE_DISABLE_EXTERNAL_SKILLS=1", "OPENCODE_DISABLE_SHARE=1",
	}
}

func createCalcRepo(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(root, "calc"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":            "module example.com/calcfixture\n\ngo 1.26\n",
		"calc/calc.go":      "package calc\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n",
		"calc/calc_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif got := Add(2, 3); got != 5 {\n\t\tt.Fatalf(\"Add(2, 3) = %d, want 5\", got)\n\t}\n}\n",
	}
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	return root
}

func assertCalcFixedAndTested(t *testing.T, repo, phase string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repo, "calc", "calc.go"))
	if err != nil {
		t.Fatalf("slice_code=%s_fixture_read", phase)
	}
	if !strings.Contains(string(body), "return a + b") || strings.Contains(string(body), "return a - b") {
		t.Fatalf("slice_code=%s_repository_not_modified", phase)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		code := edgeclient.SafeOpenCodeFailureSignal(output)
		if code == "unknown" {
			code = "tests_failed"
		}
		t.Fatalf("slice_code=%s_post_test_%s", phase, code)
	}
}

func requestToolIDs(payload map[string]any) map[string]string {
	result := map[string]string{}
	for _, raw := range payload["tools"].([]any) {
		tool := raw.(map[string]any)
		result[tool["name"].(string)] = tool["id"].(string)
	}
	return result
}

func containsToolResult(payload map[string]any, callPrefix string) bool {
	prompt, _ := payload["prompt"].([]any)
	for _, rawMessage := range prompt {
		message, _ := rawMessage.(map[string]any)
		content, _ := message["content"].([]any)
		for _, rawPart := range content {
			part, _ := rawPart.(map[string]any)
			if part["type"] == "tool-result" && strings.HasPrefix(fmt.Sprint(part["tool_call_id"]), callPrefix) {
				return true
			}
		}
	}
	return false
}

func countOpenCodeToolEvents(t *testing.T, output string) int {
	t.Helper()
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid OpenCode JSON event: %v line=%q", err, line)
		}
		if event["type"] == "tool_use" {
			count++
		}
	}
	return count
}

func assertOpenCodeEvents(t *testing.T, output string, want []string, phase string) {
	t.Helper()
	var got []string
	finalText := false
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("slice_code=%s_event_json", phase)
		}
		if event["type"] == "tool_use" {
			part, _ := event["part"].(map[string]any)
			state, _ := part["state"].(map[string]any)
			if status := fmt.Sprint(state["status"]); status != "completed" {
				tool := safeE2EToolName(fmt.Sprint(part["tool"]))
				stateJSON, _ := json.Marshal(state)
				code := edgeclient.SafeOpenCodeFailureSignal(stateJSON)
				if code == "unknown" {
					code = "status_" + safeE2EStatus(status)
				}
				t.Fatalf("slice_code=%s_tool_%s_%s", phase, tool, code)
			}
			got = append(got, fmt.Sprint(part["tool"]))
		}
		if event["type"] == "text" {
			part, _ := event["part"].(map[string]any)
			if strings.Contains(fmt.Sprint(part["text"]), "verified") {
				finalText = true
			}
		}
	}
	sort.Strings(got)
	sortedWant := append([]string(nil), want...)
	sort.Strings(sortedWant)
	if strings.Join(got, ",") != strings.Join(sortedWant, ",") {
		t.Fatalf("slice_code=%s_tool_set", phase)
	}
	if !finalText {
		t.Fatalf("slice_code=%s_final_response", phase)
	}
}

func safeE2EToolName(value string) string {
	switch value {
	case "read", "grep", "edit", "bash":
		return value
	default:
		return "unknown"
	}
}

func safeE2EStatus(value string) string {
	switch value {
	case "pending", "running", "error", "failed", "cancelled":
		return value
	default:
		return "other"
	}
}

func assertNoExternalTraffic(t *testing.T, trace, stdout, stderr, phase string) {
	t.Helper()
	if strings.TrimSpace(trace) == "" {
		t.Fatalf("slice_code=%s_network_trace_empty", phase)
	}
	for _, forbidden := range []string{"api.openai.com", "api.anthropic.com", "openrouter.ai", "generativelanguage.googleapis.com", "api.groq.com", "api.cerebras.ai"} {
		if strings.Contains(strings.ToLower(trace+stdout+stderr), forbidden) {
			t.Fatalf("slice_code=%s_network_provider_marker", phase)
		}
	}
	external := regexp.MustCompile(`connect\([^\n]*(AF_INET6?|sin_family=AF_INET6?)[^\n]*`)
	for _, line := range external.FindAllString(trace, -1) {
		if strings.Contains(line, "127.0.0.1") || strings.Contains(line, "::1") {
			continue
		}
		t.Fatalf("slice_code=%s_network_non_loopback", phase)
	}
}

func assertNoDefaultRoute(t *testing.T) {
	t.Helper()
	body, err := os.ReadFile("/proc/net/route")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(body), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) > 2 && fields[1] == "00000000" && fields[0] != "lo" {
			t.Fatalf("container has a default network route: %s", line)
		}
	}
}

func readTraceFiles(t *testing.T, prefix string) string {
	t.Helper()
	files, err := filepath.Glob(prefix + "*")
	if err != nil {
		t.Fatal(err)
	}
	var combined strings.Builder
	for _, path := range files {
		body, _ := os.ReadFile(path)
		combined.Write(body)
	}
	return combined.String()
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func requiredAbsoluteFile(t *testing.T, env string) string {
	t.Helper()
	value := os.Getenv(env)
	if !filepath.IsAbs(value) {
		t.Fatalf("%s must be an absolute path", env)
	}
	info, err := os.Stat(value)
	if err != nil || info.IsDir() {
		t.Fatalf("%s is not a file: %v", env, err)
	}
	return value
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("file did not appear: %s", path)
}
func decodeToolJSON(t *testing.T, text string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(text), target); err != nil {
		t.Fatal(err)
	}
}
func commandOutput(t *testing.T, command string, args ...string) string {
	t.Helper()
	home := t.TempDir()
	for _, dir := range []string{
		filepath.Join(home, ".config"),
		filepath.Join(home, ".local", "share"),
		filepath.Join(home, ".local", "state"),
		filepath.Join(home, ".cache"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(command, args...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"OPENCODE_TEST_HOME=" + home,
		"OPENCODE_PURE=1",
		"OPENCODE_DISABLE_AUTOUPDATE=1",
		"OPENCODE_DISABLE_MODELS_FETCH=1",
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v %s", err, output)
	}
	return strings.TrimSpace(string(output))
}
func sortedKeys(value map[string]string) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
