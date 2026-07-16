//go:build opencode_e2e

package mcpserver

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	ProcessPIDs               map[string]int   `json:"process_pids"`
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
	EdgeAuthoritativeTables   bool             `json:"edge_authoritative_tables_present"`
	LargeRequestReferenced    bool             `json:"large_request_referenced"`
}

func TestRemoteOpenCodeDistributedRelay(t *testing.T) {
	opencodeBinary := requiredAbsoluteFile(t, "OPENCODE_E2E_BIN")
	providerPath := requiredAbsoluteDirectory(t, "OPENCODE_PROVIDER_E2E_PATH")
	edgeBinary := requiredAbsoluteFile(t, "MCP_EDGE_E2E_BIN")
	driverBinary := requiredAbsoluteFile(t, "MODEL_TURN_DRIVER_E2E_BIN")
	bubblewrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Fatal("Bubblewrap is required by the remote OpenCode E2E")
	}
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
	const goalPrefix = "Inspect the bounded Go repository. Use read and grep, edit calc/calc.go so Add adds, run go test ./..., then report completion. Bounded context padding: "
	padding := int(modelturn.MaxGoalBodyBytes) - len(goalPrefix) - 1
	if padding <= 0 {
		t.Fatal("remote E2E goal prefix exceeds the allowed bound")
	}
	goal := goalPrefix + strings.Repeat("x", padding)
	if len(goal) >= int(modelturn.MaxGoalBodyBytes) {
		t.Fatal("remote E2E goal fixture exceeds the allowed bound")
	}
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
		"--bubblewrap", bubblewrapPath,
		"--wait", "30s", "--poll", "1s", "--heartbeat", "1s", "--output-limit", "1048576",
	)
	cmd.Dir = workspacePath
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
	// Killing the remote process group is part of the test harness boundary: a failed assertion must not leave driver or OpenCode children holding pipes open.
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
	processPIDs := make(map[string]int)
	largeRequestReferenced := false
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
			var edgeProcessErr error
			select {
			case edgeProcessErr = <-processDone:
			case <-time.After(5 * time.Second):
				edgeProcessErr = errors.New("mcp-edge did not terminate after terminal runtime state")
			}
			t.Fatalf("model_turn_next sequence=%d status=%s edge_failure=%s", sequence, envelope.Status, safeRemoteEdgeFailureCode(edgeProcessErr, stderr.String()))
		}
		offer := envelope.Turn
		if sequence == 1 {
			processPIDs = assertRemoteProcessIsolation(t, os.Getpid(), cmd.Process.Pid, driverBinary, opencodeBinary)
			distinctProcessCount = int64(len(processPIDs))
			processIsolationVerified = true
		}
		if int64(len(offer.RequestPayload)) > modelturn.MaxInlineRequestBytes && strings.HasPrefix(offer.RequestRef, "mb_") {
			largeRequestReferenced = true
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
		response, err := scriptedResponse(sequence, payload, "/workspace")
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
			t.Fatalf("mcp-edge failed: edge_failure=%s", safeRemoteEdgeFailureCode(err, stderr.String()))
		}
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		t.Fatalf("mcp-edge timeout: edge_failure=%s", safeRemoteEdgeFailureCode(ctx.Err(), stderr.String()))
	}

	statusText := mcpTool(t, server, meter, "model_runtime_status", map[string]any{"runtime_id": runtime.RuntimeID})
	var completed runtimePublicView
	decodeToolJSON(t, statusText, &completed)
	if completed.State != modelturn.RuntimeStateCompleted {
		t.Fatalf("runtime did not complete: state=%s edge_failure=%s", completed.State, safeRemoteEdgeFailureCode(nil, stderr.String()))
	}
	assertCalcFixedAndTested(t, workspacePath, "remote")
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
	if tables := forbiddenEdgeSQLiteTables(t, edgeState); len(tables) != 0 {
		t.Fatalf("Edge state contains authoritative tables: %v", tables)
	}
	if !largeRequestReferenced {
		t.Fatal("distributed relay never exercised request_ref for a large request")
	}
	report := remoteDistributedReport{
		Mode: "remote-distributed", Processes: []string{"mcp-devbox-server", "mcp-edge", "model-turn-driver", "opencode-1.18.1"},
		ProcessPIDs: processPIDs,
		MCPCalls:    meter.Calls, EdgeHTTPCalls: calls, MCPRequestBytes: meter.RequestBytes, MCPResponseBytes: meter.ResponseBytes,
		EdgeRequestBytes: requestBytes, EdgeResponseBytes: responseBytes, ModelTurns: stats.TurnCount, ToolExecutions: int64(len(executedCalls)),
		ToolNames: []string{"read", "grep", "edit", "bash"},
		Elapsed:   time.Since(startedAt), ExternalWait: meter.ExternalWait, InterTurnGap: meter.InterTurnGap, Retries: 0,
		VPSResultStoreBytes: directoryBytes(t, authoritativeRoot), EdgeJournalBytes: directoryBytes(t, edgeState), EdgePaths: paths,
		AuthoritativeStore: filepath.Base(authoritativeRoot), EdgeState: filepath.Base(edgeState), WorkspaceID: workspace.ID,
		RuntimeID: runtime.RuntimeID, Completed: true, RepositoryModified: true, TestsPassed: true,
		DuplicateTurns: int64(stats.TurnCount) - int64(len(turnIDs)), DuplicateConsumptions: stats.ConsumedCount - 4,
		ProcessIsolationVerified: processIsolationVerified, DistinctProcessCount: distinctProcessCount, AuthoritativeSQLiteShared: false,
		EdgeAuthoritativeTables: false, LargeRequestReferenced: largeRequestReferenced,
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

var remoteEdgeFailurePattern = regexp.MustCompile(`failure=([a-z_]+)`)

var allowedRemoteEdgeFailures = map[string]struct{}{
	"none": {}, "timeout": {}, "cancelled": {}, "kill_switch": {}, "restart_interrupted": {}, "terminal_replay": {},
	"installation_integrity": {}, "installation_version": {}, "installation_opencode": {}, "installation_provider": {}, "installation_driver": {},
	"workspace": {}, "socket": {}, "journal": {}, "lease": {}, "driver_exit": {}, "opencode_cli": {}, "opencode_provider_load": {},
	"opencode_driver_connect": {}, "opencode_permission_ptrace": {}, "opencode_permission_connect": {}, "opencode_permission_spawn": {},
	"opencode_permission_mkdir": {}, "opencode_permission_open": {}, "opencode_permission_rename": {}, "opencode_permission_remove": {},
	"opencode_permission_chmod": {}, "opencode_permission_read_dir": {}, "opencode_permission_stat": {}, "opencode_permission_write": {},
	"opencode_permission_read": {}, "opencode_permission_other": {}, "opencode_config": {}, "opencode_model": {}, "opencode_not_found": {}, "opencode_provider": {},
	"opencode_provider_auth": {}, "opencode_output_length": {}, "opencode_unknown_type": {}, "opencode_unknown_api": {},
	"opencode_unknown_timeout": {}, "opencode_unknown_connection": {}, "opencode_prompt_shape": {}, "opencode_prompt_role": {},
	"opencode_tool_shape": {}, "opencode_request_limit": {}, "opencode_runtime_status": {}, "opencode_driver_invalid_request": {},
	"opencode_request_stage": {}, "opencode_turn_create": {}, "opencode_response_wait": {},
	"opencode_driver_status": {}, "opencode_turn_identity": {}, "opencode_response_identity": {}, "opencode_response_shape": {},
	"opencode_abort": {}, "opencode_socket": {}, "opencode_unknown_error": {}, "opencode_named_error": {}, "opencode_runtime_error": {},
	"opencode_exit": {}, "relay_unavailable": {}, "relay_rejected": {}, "internal": {}, "process_exit": {}, "process_shutdown_timeout": {},
}

func safeRemoteEdgeFailureCode(processErr error, stderr string) string {
	if match := remoteEdgeFailurePattern.FindStringSubmatch(stderr); len(match) == 2 {
		if _, ok := allowedRemoteEdgeFailures[match[1]]; ok {
			return match[1]
		}
		return "internal"
	}
	if errors.Is(processErr, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(processErr, context.Canceled) {
		return "cancelled"
	}
	if processErr != nil {
		return "process_exit"
	}
	return "internal"
}

func forbiddenEdgeSQLiteTables(t *testing.T, root string) []string {
	t.Helper()
	forbidden := map[string]struct{}{"model_turns": {}, "turn_bodies": {}, "model_runtimes": {}}
	found := make(map[string]struct{})
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || filepath.Ext(info.Name()) != ".db" {
			return nil
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			return err
		}
		rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
		if err != nil {
			_ = db.Close()
			return err
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				_ = rows.Close()
				_ = db.Close()
				return err
			}
			if _, blocked := forbidden[name]; blocked {
				found[name] = struct{}{}
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			_ = db.Close()
			return err
		}
		_ = rows.Close()
		return db.Close()
	}); err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0, len(found))
	for name := range found {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func assertRemoteProcessIsolation(t *testing.T, serverPID, edgePID int, driverBinary, opencodeBinary string) map[string]int {
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
			if isRemoteOpenCodeProcess(command, opencodeBinary) {
				opencodePID = pid
			}
		}
		if driverPID > 0 && opencodePID > 0 && driverPID != opencodePID && driverPID != edgePID && opencodePID != edgePID && driverPID != serverPID && opencodePID != serverPID {
			return map[string]int{
				"mcp-devbox-server": serverPID,
				"mcp-edge":          edgePID,
				"model-turn-driver": driverPID,
				"opencode-1.18.1":   opencodePID,
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	descendants := remoteProcessDescendants(edgePID)
	driverObserved := false
	opencodeObserved := false
	for _, command := range descendants {
		if strings.Contains(command, driverBinary) || strings.Contains(command, filepath.Base(driverBinary)) {
			driverObserved = true
		}
		if isRemoteOpenCodeProcess(command, opencodeBinary) {
			opencodeObserved = true
		}
	}
	switch {
	case !driverObserved && !opencodeObserved:
		t.Fatal("slice_code=remote_isolation_both_missing")
	case !driverObserved:
		t.Fatal("slice_code=remote_isolation_driver_missing")
	case !opencodeObserved:
		t.Fatal("slice_code=remote_isolation_opencode_missing")
	default:
		t.Fatal("slice_code=remote_isolation_identity")
	}
	return nil
}

func isRemoteOpenCodeProcess(command, opencodeBinary string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 || filepath.Base(fields[0]) == "bwrap" {
		return false
	}
	return strings.Contains(command, opencodeBinary) || strings.Contains(command, "opencode-ai") || strings.Contains(command, "/mcp-opencode")
}

func remoteProcessDescendants(root int) map[int]string {
	return remoteProcessDescendantsAt("/proc", root)
}

func remoteProcessDescendantsAt(procRoot string, root int) map[int]string {
	results := make(map[int]string)
	queue := []int{root}
	seen := map[int]struct{}{root: {}}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		taskRoot := filepath.Join(procRoot, strconv.Itoa(pid), "task")
		tasks, err := os.ReadDir(taskRoot)
		if err != nil {
			continue
		}
		for _, task := range tasks {
			if !task.IsDir() {
				continue
			}
			childrenBody, err := os.ReadFile(filepath.Join(taskRoot, task.Name(), "children"))
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
				cmdline, _ := os.ReadFile(filepath.Join(procRoot, field, "cmdline"))
				results[child] = strings.ReplaceAll(string(cmdline), "\x00", " ")
				queue = append(queue, child)
			}
		}
	}
	return results
}

func TestRemoteOpenCodeProcessDetectionExcludesBubblewrapParent(t *testing.T) {
	opencodePath := "/opt/opencode/node_modules/opencode-ai/bin/opencode"
	bubblewrapCommand := "/usr/bin/bwrap --unshare-all -- /mcp-opencode run --dir /workspace"
	if isRemoteOpenCodeProcess(bubblewrapCommand, opencodePath) {
		t.Fatal("Bubblewrap parent was mistaken for OpenCode")
	}
	for _, command := range []string{
		opencodePath + " run --dir /workspace",
		"/usr/bin/node /mcp-opencode run --dir /workspace",
	} {
		if !isRemoteOpenCodeProcess(command, opencodePath) {
			t.Fatalf("OpenCode process was not detected: %q", command)
		}
	}
}

func TestRemoteProcessDescendantsReadsAllTaskThreads(t *testing.T) {
	procRoot := t.TempDir()
	for _, path := range []string{
		filepath.Join(procRoot, "100", "task", "100"),
		filepath.Join(procRoot, "100", "task", "101"),
		filepath.Join(procRoot, "200", "task", "200"),
		filepath.Join(procRoot, "300", "task", "300"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(procRoot, "100", "task", "100", "children"): "",
		filepath.Join(procRoot, "100", "task", "101", "children"): "200\n",
		filepath.Join(procRoot, "200", "task", "200", "children"): "300\n",
		filepath.Join(procRoot, "300", "task", "300", "children"): "",
		filepath.Join(procRoot, "200", "cmdline"):                 "model-turn-driver\x00--remote\x00",
		filepath.Join(procRoot, "300", "cmdline"):                 "opencode\x00run\x00",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	descendants := remoteProcessDescendantsAt(procRoot, 100)
	if descendants[200] != "model-turn-driver --remote " || descendants[300] != "opencode run " || len(descendants) != 2 {
		t.Fatalf("descendants=%v", descendants)
	}
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

func requiredAbsoluteDirectory(t *testing.T, env string) string {
	t.Helper()
	value := os.Getenv(env)
	if !filepath.IsAbs(value) {
		t.Fatalf("%s must be an absolute path", env)
	}
	info, err := os.Stat(value)
	if err != nil || !info.IsDir() {
		t.Fatalf("%s is not a directory: %v", env, err)
	}
	return value
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
