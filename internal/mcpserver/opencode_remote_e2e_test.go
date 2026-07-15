//go:build opencode_e2e

package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

type remoteWireMeter struct {
	mu            sync.Mutex
	calls         int64
	requestBytes  int64
	responseBytes int64
	paths         map[string]int64
}

type remoteCountingWriter struct {
	http.ResponseWriter
	meter *remoteWireMeter
}

func (w remoteCountingWriter) Write(payload []byte) (int, error) {
	written, err := w.ResponseWriter.Write(payload)
	w.meter.mu.Lock()
	w.meter.responseBytes += int64(written)
	w.meter.mu.Unlock()
	return written, err
}

func (m *remoteWireMeter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		m.mu.Lock()
		m.calls++
		m.paths[request.URL.Path]++
		if request.ContentLength > 0 {
			m.requestBytes += request.ContentLength
		}
		m.mu.Unlock()
		next.ServeHTTP(remoteCountingWriter{ResponseWriter: writer, meter: m}, request)
	})
}

func (m *remoteWireMeter) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = 0
	m.requestBytes = 0
	m.responseBytes = 0
	m.paths = make(map[string]int64)
}

func (m *remoteWireMeter) Snapshot() (int64, int64, int64, map[string]int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	paths := make(map[string]int64, len(m.paths))
	for path, count := range m.paths {
		paths[path] = count
	}
	return m.calls, m.requestBytes, m.responseBytes, paths
}

type remoteDistributedReport struct {
	Mode                      string           `json:"mode"`
	Processes                 []string         `json:"processes"`
	MCPCalls                  int64            `json:"mcp_calls"`
	EdgeHTTPCalls             int64            `json:"edge_http_calls"`
	MCPRequestBytes           int64            `json:"mcp_request_bytes"`
	MCPResponseBytes          int64            `json:"mcp_response_bytes"`
	EdgeRequestBytes          int64            `json:"edge_request_bytes"`
	EdgeResponseBytes         int64            `json:"edge_response_bytes"`
	ModelTurns                int64            `json:"model_turns"`
	ToolExecutions            int64            `json:"tool_executions"`
	ToolNames                 []string         `json:"tool_names"`
	Elapsed                   time.Duration    `json:"elapsed"`
	ExternalWait              time.Duration    `json:"external_wait"`
	InterTurnGap              time.Duration    `json:"inter_turn_gap"`
	Retries                   int64            `json:"retries"`
	VPSResultStoreBytes       int64            `json:"vps_result_store_bytes"`
	EdgeJournalBytes          int64            `json:"edge_journal_bytes"`
	EdgePaths                 map[string]int64 `json:"edge_paths"`
	AuthoritativeStore        string           `json:"authoritative_store"`
	EdgeState                 string           `json:"edge_state"`
	WorkspaceID               string           `json:"workspace_id"`
	RuntimeID                 string           `json:"runtime_id"`
	Completed                 bool             `json:"completed"`
	RepositoryModified        bool             `json:"repository_modified"`
	TestsPassed               bool             `json:"tests_passed"`
	DuplicateTurns            int64            `json:"duplicate_turns"`
	DuplicateConsumptions     int64            `json:"duplicate_consumptions"`
	ProcessIsolationVerified  bool             `json:"process_isolation_verified"`
	DistinctProcessCount      int64            `json:"distinct_process_count"`
	AuthoritativeSQLiteShared bool             `json:"authoritative_sqlite_shared"`
}

func TestRemoteOpenCodeDistributedRelay(t *testing.T) {
	opencodeBinary := requiredAbsoluteFile(t, "OPENCODE_E2E_BIN")
	providerPath := requiredAbsoluteFile(t, "OPENCODE_PROVIDER_E2E_PATH")
	edgeBinary := requiredAbsoluteFile(t, "MCP_EDGE_E2E_BIN")
	driverBinary := requiredAbsoluteFile(t, "MODEL_TURN_DRIVER_E2E_BIN")
	integrityPath := filepath.Join(repoRoot(t), "test", "opencode-e2e", "package-lock.json")
	if _, err := os.Stat(integrityPath); err != nil {
		t.Fatal(err)
	}

	authoritativeRoot := filepath.Join(t.TempDir(), "authoritative")
	devices, err := edge.Open(edge.Config{Root: filepath.Join(authoritativeRoot, "edge")})
	if err != nil {
		t.Fatal(err)
	}
	defer devices.Close()
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(authoritativeRoot, "model-turns")})
	if err != nil {
		t.Fatal(err)
	}
	defer turns.Close()

	wire := &remoteWireMeter{paths: make(map[string]int64)}
	httpServer := httptest.NewTLSServer(wire.Handler(edge.NewHTTPHandler(devices, turns)))
	defer httpServer.Close()
	caPath := filepath.Join(t.TempDir(), "relay-ca.pem")
	caBody := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: httpServer.Certificate().Raw})
	if err := os.WriteFile(caPath, caBody, 0o644); err != nil {
		t.Fatal(err)
	}

	edgeState := filepath.Join(t.TempDir(), "edge-state")
	pairingCode, err := devices.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := edgeclient.Pair(t.Context(), edgeclient.PairOptions{
		ServerURL: httpServer.URL, Code: pairingCode, Name: "parrot-e2e", StateRoot: edgeState, HTTPClient: httpServer.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	workspacePath := createCalcRepo(t, "remote-distributed")
	registry, err := edgeclient.OpenWorkspaceRegistry(edgeState)
	if err != nil {
		t.Fatal(err)
	}
	workspace, created, err := registry.Add(workspacePath)
	if err != nil || !created {
		t.Fatalf("workspace=%+v created=%t err=%v", workspace, created, err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	wire.Reset()

	server, _, _ := e2eServer(t, authoritativeRoot, turns)
	server.WithEdgeStore(devices)
	meter := &e2eMeter{}
	goal := "Inspect the bounded Go repository. Use read and grep, edit calc/calc.go so Add adds, run go test ./..., then report completion."
	startText := mcpTool(t, server, meter, "opencode_runtime_start", map[string]any{
		"device_id": identity.DeviceID, "workspace_id": workspace.ID, "goal": goal,
		"timeout_seconds": 180, "idempotency_key": "remote-distributed-opencode-e2e",
	})
	var runtime runtimePublicView
	decodeToolJSON(t, startText, &runtime)
	if runtime.State != modelturn.RuntimeStateAwaitingEdge || runtime.DeviceID != identity.DeviceID || runtime.WorkspaceID != workspace.ID {
		t.Fatalf("runtime=%+v", runtime)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, edgeBinary,
		"opencode", "--once", "--state", edgeState,
		"--opencode", opencodeBinary, "--driver", driverBinary,
		"--provider", providerPath, "--integrity", integrityPath,
		"--wait", "30s", "--poll", "1s", "--heartbeat", "1s", "--output-limit", "1048576",
	)
	cmd.Dir = workspacePath
	home := filepath.Join(t.TempDir(), "edge-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "USER=mcpedge", "LANG=C.UTF-8", "LC_ALL=C.UTF-8",
		"SSL_CERT_FILE=" + caPath,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	processDone := make(chan error, 1)
	go func() { processDone <- cmd.Wait() }()

	lastResponded := time.Time{}
	turnIDs := make(map[modelturn.TurnID]struct{})
	executedCalls := make(map[string]string)
	processIsolationVerified := false
	distinctProcessCount := int64(0)
	for sequence := uint64(1); sequence <= 4; sequence++ {
		turnText := mcpTool(t, server, meter, "model_turn_next", map[string]any{
			"runtime_id": runtime.RuntimeID, "after_sequence": sequence - 1, "wait_seconds": 60,
		})
		var envelope struct {
			Pending bool            `json:"pending"`
			Status  string          `json:"status"`
			Turn    modelturn.Offer `json:"turn"`
		}
		decodeToolJSON(t, turnText, &envelope)
		if !envelope.Pending || envelope.Status != "turn" {
			t.Fatalf("model_turn_next sequence=%d returned %+v", sequence, envelope)
		}
		offer := envelope.Turn
		if sequence == 1 {
			distinctProcessCount = int64(assertRemoteProcessIsolation(t, os.Getpid(), cmd.Process.Pid, driverBinary, opencodeBinary))
			processIsolationVerified = true
		}
		if offer.Sequence != sequence || offer.RuntimeID != runtime.RuntimeID {
			t.Fatalf("turn identity mismatch: %+v", offer.Record)
		}
		if _, duplicate := turnIDs[offer.TurnID]; duplicate {
			t.Fatalf("duplicate turn id: %s", offer.TurnID)
		}
		turnIDs[offer.TurnID] = struct{}{}
		meter.ExternalWait += time.Since(offer.CreatedAt)
		if !lastResponded.IsZero() && offer.CreatedAt.After(lastResponded) {
			meter.InterTurnGap += offer.CreatedAt.Sub(lastResponded)
		}
		var payload map[string]any
		if err := json.Unmarshal(offer.RequestPayload, &payload); err != nil {
			t.Fatal(err)
		}
		if sequence > 1 && !containsToolResult(payload, fmt.Sprintf("turn-%d", sequence-1)) {
			t.Fatalf("turn %d omitted prior tool result", sequence)
		}
		for callID, name := range remoteExecutedToolResults(payload) {
			executedCalls[callID] = name
		}
		response, err := scriptedResponse(sequence, payload, workspacePath)
		if err != nil {
			t.Fatal(err)
		}
		mcpTool(t, server, meter, "model_turn_respond", map[string]any{
			"runtime_id": runtime.RuntimeID, "turn_id": string(offer.TurnID),
			"expected_sequence": offer.Sequence, "request_digest": offer.RequestDigest, "response": response,
		})
		lastResponded = time.Now()
	}

	select {
	case err := <-processDone:
		if err != nil {
			t.Fatalf("mcp-edge failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
		}
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		t.Fatalf("mcp-edge timeout: %v\nstdout=%s\nstderr=%s", ctx.Err(), stdout.String(), stderr.String())
	}

	statusText := mcpTool(t, server, meter, "model_runtime_status", map[string]any{"runtime_id": runtime.RuntimeID})
	var completed runtimePublicView
	decodeToolJSON(t, statusText, &completed)
	if completed.State != modelturn.RuntimeStateCompleted {
		t.Fatalf("runtime did not complete: %+v stdout=%s stderr=%s", completed, stdout.String(), stderr.String())
	}
	assertCalcFixedAndTested(t, workspacePath)
	stats, err := turns.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if stats.RuntimeCount != 1 || stats.TurnCount != 4 || stats.ConsumedCount != 4 || stats.AwaitingCount != 0 || stats.RespondedCount != 0 {
		t.Fatalf("authoritative stats=%+v", stats)
	}
	calls, requestBytes, responseBytes, paths := wire.Snapshot()
	createCalls, waitCalls := int64(0), int64(0)
	for path, count := range paths {
		switch {
		case strings.HasSuffix(path, "/turns"):
			createCalls += count
		case strings.HasSuffix(path, "/wait"):
			waitCalls += count
		}
	}
	if createCalls != 4 || waitCalls != 4 {
		t.Fatalf("normal relay retried or omitted turns: create=%d wait=%d paths=%v", createCalls, waitCalls, paths)
	}
	if meter.Calls >= 20 || meter.Calls != 10 {
		t.Fatalf("MCP/control calls=%d want=10 and <20", meter.Calls)
	}
	wantExecuted := map[string]string{
		"turn-1-read": "read", "turn-1-grep": "grep", "turn-2": "edit", "turn-3": "bash",
	}
	if len(executedCalls) != len(wantExecuted) {
		t.Fatalf("actual OpenCode tool results=%v want=%v", executedCalls, wantExecuted)
	}
	for callID, name := range wantExecuted {
		if executedCalls[callID] != name {
			t.Fatalf("actual OpenCode tool result %s=%q want=%q all=%v", callID, executedCalls[callID], name, executedCalls)
		}
	}
	if authoritativeRoot == edgeState || strings.HasPrefix(edgeState, authoritativeRoot+string(os.PathSeparator)) || strings.HasPrefix(authoritativeRoot, edgeState+string(os.PathSeparator)) {
		t.Fatalf("authoritative and Edge roots overlap: authoritative=%s edge=%s", authoritativeRoot, edgeState)
	}
	if _, err := os.Stat(filepath.Join(authoritativeRoot, "model-turns", "model-turns.db")); err != nil {
		t.Fatalf("authoritative model-turn SQLite missing: %v", err)
	}
	if namedFileExists(t, edgeState, "model-turns.db") {
		t.Fatal("Edge state contains authoritative model-turn SQLite")
	}
	report := remoteDistributedReport{
		Mode: "remote-distributed", Processes: []string{"mcp-devbox-server", "mcp-edge", "model-turn-driver", "opencode-1.18.1"},
		MCPCalls: meter.Calls, EdgeHTTPCalls: calls, MCPRequestBytes: meter.RequestBytes, MCPResponseBytes: meter.ResponseBytes,
		EdgeRequestBytes: requestBytes, EdgeResponseBytes: responseBytes, ModelTurns: stats.TurnCount, ToolExecutions: int64(len(executedCalls)),
		ToolNames: []string{"read", "grep", "edit", "bash"},
		Elapsed:   time.Since(startedAt), ExternalWait: meter.ExternalWait, InterTurnGap: meter.InterTurnGap, Retries: 0,
		VPSResultStoreBytes: directoryBytes(t, authoritativeRoot), EdgeJournalBytes: directoryBytes(t, edgeState), EdgePaths: paths,
		AuthoritativeStore: filepath.Base(authoritativeRoot), EdgeState: filepath.Base(edgeState), WorkspaceID: workspace.ID,
		RuntimeID: runtime.RuntimeID, Completed: true, RepositoryModified: true, TestsPassed: true,
		DuplicateTurns: int64(stats.TurnCount) - int64(len(turnIDs)), DuplicateConsumptions: stats.ConsumedCount - 4,
		ProcessIsolationVerified: processIsolationVerified, DistinctProcessCount: distinctProcessCount, AuthoritativeSQLiteShared: false,
	}
	if report.DuplicateTurns != 0 || report.DuplicateConsumptions != 0 {
		t.Fatalf("duplicate execution evidence: %+v", report)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(goal)) || bytes.Contains(encoded, []byte(workspacePath)) || bytes.Contains(encoded, []byte(httpServer.URL)) {
		t.Fatalf("remote report leaked private goal, path, or server URL: %s", encoded)
	}
	artifactDir := filepath.Join(repoRoot(t), "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(artifactDir, "opencode-remote-e2e-report.json")
	if err := os.WriteFile(artifact, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("REMOTE_DISTRIBUTED_REPORT=%s", encoded)
}

func assertRemoteProcessIsolation(t *testing.T, serverPID, edgePID int, driverBinary, opencodeBinary string) int {
	t.Helper()
	if serverPID <= 0 || edgePID <= 0 || serverPID == edgePID {
		t.Fatalf("server and Edge processes are not distinct: server=%d edge=%d", serverPID, edgePID)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		descendants := remoteProcessDescendants(edgePID)
		driverPID := 0
		opencodePID := 0
		for pid, command := range descendants {
			if strings.Contains(command, driverBinary) || strings.Contains(command, filepath.Base(driverBinary)) {
				driverPID = pid
			}
			if strings.Contains(command, opencodeBinary) || strings.Contains(command, "opencode-ai") || strings.Contains(command, "/opencode") {
				opencodePID = pid
			}
		}
		if driverPID > 0 && opencodePID > 0 && driverPID != opencodePID && driverPID != edgePID && opencodePID != edgePID && driverPID != serverPID && opencodePID != serverPID {
			return 4
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("four-process isolation not observed for Edge pid %d", edgePID)
	return 0
}

func remoteProcessDescendants(root int) map[int]string {
	results := make(map[int]string)
	queue := []int{root}
	seen := map[int]struct{}{root: {}}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		childrenPath := filepath.Join("/proc", strconv.Itoa(pid), "task", strconv.Itoa(pid), "children")
		childrenBody, err := os.ReadFile(childrenPath)
		if err != nil {
			continue
		}
		for _, field := range strings.Fields(string(childrenBody)) {
			child, err := strconv.Atoi(field)
			if err != nil {
				continue
			}
			if _, duplicate := seen[child]; duplicate {
				continue
			}
			seen[child] = struct{}{}
			cmdline, _ := os.ReadFile(filepath.Join("/proc", field, "cmdline"))
			results[child] = strings.ReplaceAll(string(cmdline), "\x00", " ")
			queue = append(queue, child)
		}
	}
	return results
}

func namedFileExists(t *testing.T, root, name string) bool {
	t.Helper()
	found := false
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && info.Name() == name {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return found
}

func remoteExecutedToolResults(payload map[string]any) map[string]string {
	results := make(map[string]string)
	prompt, _ := payload["prompt"].([]any)
	for _, rawMessage := range prompt {
		message, _ := rawMessage.(map[string]any)
		content, _ := message["content"].([]any)
		for _, rawPart := range content {
			part, _ := rawPart.(map[string]any)
			if part["type"] != "tool-result" {
				continue
			}
			callID := fmt.Sprint(part["tool_call_id"])
			if callID == "" {
				callID = fmt.Sprint(part["toolCallId"])
			}
			name := fmt.Sprint(part["tool_name"])
			if name == "" {
				name = fmt.Sprint(part["toolName"])
			}
			if name == "" {
				switch callID {
				case "turn-1-read":
					name = "read"
				case "turn-1-grep":
					name = "grep"
				case "turn-2":
					name = "edit"
				case "turn-3":
					name = "bash"
				}
			}
			if callID != "" && name != "" {
				results[callID] = name
			}
		}
	}
	return results
}

func directoryBytes(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	if err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return total
}

var _ io.Writer = remoteCountingWriter{}
