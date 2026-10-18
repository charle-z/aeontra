//go:build !windows

package provider_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	stageTransportBuild   = "transport_build"
	stageLeaseBinding     = "lease_binding"
	stageRuntimeStatus    = "runtime_status"
	stageSocketConnect    = "socket_connect"
	stageRequestStage     = "request_stage"
	stageTurnCreate       = "turn_create"
	stageResponseWait     = "response_wait"
	stageResponseIdentity = "response_identity"
	stageResponseConsume  = "response_consume"
)

var (
	providerRepoRoot    string
	providerDriverBin   string
	remoteCreatePattern = regexp.MustCompile(`^ec_[a-f0-9]{32}$`)
	remoteWaitPattern   = regexp.MustCompile(`^ew_[a-f0-9]{32}$`)
)

func TestMain(m *testing.M) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, stageTransportBuild)
		os.Exit(2)
	}
	providerRepoRoot = filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	buildRoot, err := os.MkdirTemp("", "mcp-devbox-driver-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, stageTransportBuild)
		os.Exit(2)
	}
	providerDriverBin = filepath.Join(buildRoot, "model-turn-driver")
	command := exec.Command("go", "build", "-o", providerDriverBin, "./cmd/model-turn-driver")
	command.Dir = providerRepoRoot
	if err := command.Run(); err != nil {
		_ = os.RemoveAll(buildRoot)
		fmt.Fprintln(os.Stderr, stageTransportBuild)
		os.Exit(2)
	}
	code := m.Run()
	_ = os.RemoveAll(buildRoot)
	os.Exit(code)
}

type remoteProviderFixture struct {
	turns       *modelturn.Store
	server      *httptest.Server
	audit       *remoteRequestAudit
	edgeState   string
	caPath      string
	lease       edgeclient.ModelRuntimeLease
	runtime     modelturn.Runtime
	workspaceID string
}

type remoteDriverProcess struct {
	command *exec.Cmd
	done    chan error
	cancel  context.CancelFunc
}

type remoteProviderResult struct {
	Status          string `json:"status"`
	RuntimeCalls    int    `json:"runtime_calls"`
	StageCalls      int    `json:"stage_calls"`
	CreateCalls     int    `json:"create_calls"`
	WaitCalls       int    `json:"wait_calls"`
	UnexpectedCalls int    `json:"unexpected_calls"`
	SameWaitPath    bool   `json:"same_wait_path"`
}

type journalTurn struct {
	RuntimeID     string
	Sequence      uint64
	RequestDigest string
	CreateID      string
	TurnID        string
	RequestRef    string
	WaitID        string
	State         string
}

type remoteRequestAudit struct {
	next     http.Handler
	mu       sync.Mutex
	deviceID string
	counts   map[string]int
	active   map[string]int
	nonces   map[string]struct{}
	valid    bool
}

func TestNodeProviderAgainstRemoteEdgeTransport(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		large      bool
		stageCalls int
	}{
		{name: "inline", large: false, stageCalls: 0},
		{name: "request_ref", large: true, stageCalls: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newRemoteProviderFixture(t, testCase.name)
			driver := startRemoteProviderDriver(t, fixture)
			waitForProviderSocket(t, filepath.Join(fixture.edgeState, "provider.sock"))

			command := startRemoteNodeProvider(t, fixture, testCase.large, "accepted")
			offer := nextRemoteOffer(t, fixture.turns, fixture.runtime.RuntimeID)
			if offer.RuntimeID != fixture.runtime.RuntimeID || offer.Sequence != 1 || offer.RequestDigest == "" || offer.TurnID == "" || offer.RequestRef == "" {
				t.Fatal(stageTurnCreate)
			}
			respondRemoteOffer(t, fixture.turns, offer, "accepted")
			result := waitRemoteNodeProvider(t, command)
			if result.Status != "ok" || result.RuntimeCalls != 1 || result.StageCalls != testCase.stageCalls || result.CreateCalls != 1 || result.WaitCalls != 1 || result.UnexpectedCalls != 0 || !result.SameWaitPath {
				t.Fatal(closedResultStage(result.Status))
			}
			stopRemoteProviderDriver(t, driver)

			entry, stagedBodies, forbiddenTables, leaseCount := readRemoteJournal(t, fixture.edgeState)
			if entry.RuntimeID != offer.RuntimeID || entry.Sequence != offer.Sequence || entry.RequestDigest != offer.RequestDigest || entry.TurnID != string(offer.TurnID) || entry.RequestRef != offer.RequestRef || entry.State != "consumed" || !remoteCreatePattern.MatchString(entry.CreateID) || !remoteWaitPattern.MatchString(entry.WaitID) {
				t.Fatal(stageResponseIdentity)
			}
			if stagedBodies != 0 || forbiddenTables != 0 {
				t.Fatal(stageRequestStage)
			}
			if leaseCount != 1 {
				t.Fatal(stageLeaseBinding)
			}
			assertRemoteStoreConsumedOnce(t, fixture.turns, offer)
			if !fixture.audit.ok() || fixture.audit.count(stageLeaseBinding) != 1 || fixture.audit.count(stageRuntimeStatus) != 1 || fixture.audit.count(stageTurnCreate) != 1 || fixture.audit.count(stageResponseWait) != 1 {
				t.Fatal(stageResponseIdentity)
			}
		})
	}
}

func TestNodeProviderRetriesRemoteResponseAfterDriverRestart(t *testing.T) {
	fixture := newRemoteProviderFixture(t, "restart")
	socketPath := filepath.Join(fixture.edgeState, "provider.sock")
	first := startRemoteProviderDriver(t, fixture)
	waitForProviderSocket(t, socketPath)
	command := startRemoteNodeProvider(t, fixture, true, "resumed")
	offer := nextRemoteOffer(t, fixture.turns, fixture.runtime.RuntimeID)
	waitForProviderDriverWait(t, socketPath, fixture.audit)
	firstMetrics, err := readProviderDriverMetrics(socketPath)
	if err != nil || firstMetrics.CreateCalls != 1 || firstMetrics.WaitCalls != 1 {
		t.Fatal(stageResponseWait)
	}
	stopRemoteProviderDriver(t, first)
	waitForRemoteWaitRelease(t, fixture.audit)
	before, _, forbiddenTables, leaseCount := readRemoteJournal(t, fixture.edgeState)
	if before.RuntimeID != offer.RuntimeID || before.Sequence != offer.Sequence || before.RequestDigest != offer.RequestDigest || before.TurnID != string(offer.TurnID) || before.RequestRef != offer.RequestRef || before.State != "awaiting_response" || !remoteCreatePattern.MatchString(before.CreateID) || !remoteWaitPattern.MatchString(before.WaitID) || forbiddenTables != 0 || leaseCount != 1 {
		t.Fatal(stageResponseIdentity)
	}

	respondRemoteOffer(t, fixture.turns, offer, "resumed")
	second := startRemoteProviderDriver(t, fixture)
	waitForProviderSocket(t, socketPath)
	result := waitRemoteNodeProvider(t, command)
	if result.Status != "ok" || result.RuntimeCalls != 1 || result.StageCalls != 1 || result.CreateCalls != 1 || result.WaitCalls < 2 || result.WaitCalls > 6 || result.UnexpectedCalls != 0 || !result.SameWaitPath {
		t.Fatal(closedResultStage(result.Status))
	}
	secondMetrics, err := readProviderDriverMetrics(socketPath)
	if err != nil || secondMetrics.CreateCalls != 0 || secondMetrics.WaitCalls != 1 {
		t.Fatal(stageResponseWait)
	}
	stopRemoteProviderDriver(t, second)

	after, stagedBodies, forbiddenTables, leaseCount := readRemoteJournal(t, fixture.edgeState)
	if before.RuntimeID != after.RuntimeID || before.Sequence != after.Sequence || before.RequestDigest != after.RequestDigest || before.CreateID != after.CreateID || before.TurnID != after.TurnID || before.RequestRef != after.RequestRef || before.WaitID != after.WaitID || after.State != "consumed" {
		t.Fatal(stageResponseIdentity)
	}
	if stagedBodies != 0 || forbiddenTables != 0 || leaseCount != 1 {
		t.Fatal(stageRequestStage)
	}
	assertRemoteStoreConsumedOnce(t, fixture.turns, offer)
	if !fixture.audit.ok() || fixture.audit.count(stageLeaseBinding) != 1 || fixture.audit.count(stageRuntimeStatus) != 1 || fixture.audit.count(stageTurnCreate) != 1 || fixture.audit.count(stageResponseWait) != 2 {
		t.Fatal(stageResponseWait)
	}
}

func TestNodeProviderPreservesFourTurnRemoteToolConversation(t *testing.T) {
	fixture := newRemoteProviderFixture(t, "multiturn")
	driver := startRemoteProviderDriver(t, fixture)
	defer stopRemoteProviderDriver(t, driver)
	waitForProviderSocket(t, filepath.Join(fixture.edgeState, "provider.sock"))
	command := startRemoteNodeProviderConversation(t, fixture)
	priorCallPrefix := ""
	for sequence := uint64(1); sequence <= 4; sequence++ {
		offer := nextRemoteOffer(t, fixture.turns, fixture.runtime.RuntimeID)
		if offer.Sequence != sequence || offer.RuntimeID != fixture.runtime.RuntimeID {
			t.Fatal(stageTurnCreate)
		}
		if priorCallPrefix != "" && !bytes.Contains(offer.RequestPayload, []byte(priorCallPrefix)) {
			t.Fatal(stageResponseConsume)
		}
		payload, used := remoteConversationResponse(t, sequence, offer.RequestPayload)
		if _, err := fixture.turns.Respond(t.Context(), modelturn.ResponseSubmission{
			RuntimeID: offer.RuntimeID, TurnID: offer.TurnID, ExpectedSequence: offer.Sequence,
			RequestDigest: offer.RequestDigest, Payload: payload, UsedToolIDs: used,
		}); err != nil {
			t.Fatal(stageResponseWait)
		}
		switch sequence {
		case 1:
			priorCallPrefix = "turn-1"
		case 2:
			priorCallPrefix = "turn-2"
		case 3:
			priorCallPrefix = "turn-3"
		}
	}
	result := waitRemoteNodeProvider(t, command)
	if result.Status != "ok" || result.RuntimeCalls != 4 || result.StageCalls != 0 || result.CreateCalls != 4 || result.WaitCalls != 4 || result.UnexpectedCalls != 0 {
		t.Fatal(closedResultStage(result.Status))
	}
	stats, err := fixture.turns.Stats(t.Context())
	if err != nil || stats.RuntimeCount != 1 || stats.TurnCount != 4 || stats.ConsumedCount != 4 || stats.AwaitingCount != 0 || stats.RespondedCount != 0 {
		t.Fatal(stageResponseConsume)
	}
	if !fixture.audit.ok() || fixture.audit.count(stageTurnCreate) != 4 || fixture.audit.count(stageResponseWait) != 4 {
		t.Fatal(stageResponseIdentity)
	}
}

func startRemoteNodeProviderConversation(t *testing.T, fixture remoteProviderFixture) *exec.Cmd {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(stageSocketConnect)
	}
	scriptPath := filepath.Join(t.TempDir(), "provider-remote-multiturn.mjs")
	if err := os.WriteFile(scriptPath, []byte(remoteProviderMultiTurnScript), 0o600); err != nil {
		t.Fatal(stageTransportBuild)
	}
	providerPath := filepath.Join(providerRepoRoot, "integrations", "opencode", "provider", "index.js")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, node, scriptPath, providerPath, filepath.Join(fixture.edgeState, "provider.sock"), fixture.runtime.RuntimeID)
	command.Dir = filepath.Dir(providerPath)
	command.Stdout = &bytes.Buffer{}
	command.Stderr = ioDiscard{}
	if err := command.Start(); err != nil {
		t.Fatal(stageSocketConnect)
	}
	return command
}

func remoteConversationResponse(t *testing.T, sequence uint64, raw json.RawMessage) (json.RawMessage, []string) {
	t.Helper()
	var request struct {
		Tools []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(stageResponseIdentity)
	}
	ids := make(map[string]string, len(request.Tools))
	for _, tool := range request.Tools {
		ids[tool.Name] = tool.ID
	}
	call := func(callID, name string, arguments map[string]any) map[string]any {
		id := ids[name]
		if id == "" {
			t.Fatal(stageResponseIdentity)
		}
		return map[string]any{"call_id": callID, "tool_id": id, "arguments": arguments}
	}
	response := map[string]any{"text": "", "usage": nil}
	var used []string
	switch sequence {
	case 1:
		calls := []any{
			call("turn-1-read", "read", map[string]any{"filePath": "calc/calc.go"}),
			call("turn-1-grep", "grep", map[string]any{"pattern": "return a - b", "path": "."}),
		}
		response["finish_reason"] = "tool_calls"
		response["tool_calls"] = calls
		used = []string{ids["read"], ids["grep"]}
	case 2:
		response["finish_reason"] = "tool_calls"
		response["tool_calls"] = []any{call("turn-2", "edit", map[string]any{"filePath": "calc/calc.go", "oldString": "return a - b", "newString": "return a + b"})}
		used = []string{ids["edit"]}
	case 3:
		response["finish_reason"] = "tool_calls"
		response["tool_calls"] = []any{call("turn-3", "bash", map[string]any{"command": "go test ./..."})}
		used = []string{ids["bash"]}
	case 4:
		response["text"] = "verified"
		response["finish_reason"] = "stop"
		response["tool_calls"] = []any{}
	default:
		t.Fatal(stageResponseIdentity)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(stageResponseIdentity)
	}
	return encoded, used
}

const remoteProviderMultiTurnScript = `
import { pathToFileURL } from "node:url";
const { createMCPDevboxModelBridge, __test } = await import(pathToFileURL(process.argv[2]).href);
const socketPath = process.argv[3];
const runtimeID = process.argv[4];
const base = __test.createUnixRequester(socketPath);
const counts = {runtime: 0, stage: 0, create: 0, wait: 0, unexpected: 0};
const requestImpl = async (request) => {
  if (request.method === "GET" && request.path === "/v1/runtimes/" + runtimeID) counts.runtime += 1;
  else if (request.method === "POST" && request.path === "/v1/request-bodies") counts.stage += 1;
  else if (request.method === "POST" && request.path === "/v1/turns") counts.create += 1;
  else if (request.method === "GET" && request.path.endsWith("/response")) counts.wait += 1;
  else counts.unexpected += 1;
  return base(request);
};
const tools = [
  {type:"function",name:"read",description:"read",inputSchema:{type:"object",properties:{filePath:{type:"string"}},required:["filePath"]}},
  {type:"function",name:"grep",description:"grep",inputSchema:{type:"object",properties:{pattern:{type:"string"},path:{type:"string"}},required:["pattern","path"]}},
  {type:"function",name:"edit",description:"edit",inputSchema:{type:"object",properties:{filePath:{type:"string"},oldString:{type:"string"},newString:{type:"string"}},required:["filePath","oldString","newString"]}},
  {type:"function",name:"bash",description:"bash",inputSchema:{type:"object",properties:{command:{type:"string"}},required:["command"]}},
];
const model = createMCPDevboxModelBridge({socketPath,runtimeID,requestImpl,ttlMs:60000,timeoutMs:20000}).languageModel("external-model");
const prompt = [{role:"user",content:[{type:"text",text:"fix and verify"}]}];
let status = "ok";
try {
  for (let sequence = 1; sequence <= 4; sequence += 1) {
    const result = await model.doGenerate({prompt,tools});
    if (sequence === 4) {
      if (result.finishReason.unified !== "stop" || !result.content.some((part) => part.type === "text" && part.text === "verified")) status = "response_identity";
      break;
    }
    const calls = result.content.filter((part) => part.type === "tool-call");
    const expected = sequence === 1 ? 2 : 1;
    if (calls.length !== expected) { status = "response_identity"; break; }
    prompt.push({role:"assistant",content:calls.map((call) => ({type:"tool-call",toolCallId:call.toolCallId,toolName:call.toolName,input:JSON.parse(call.input)}))});
    prompt.push({role:"tool",content:calls.map((call) => ({type:"tool-result",toolCallId:call.toolCallId,toolName:call.toolName,output:{type:"text",value:"completed"}}))});
  }
} catch (error) {
  status = error?.mcpStage ?? "response_identity";
}
process.stdout.write(JSON.stringify({status,runtime_calls:counts.runtime,stage_calls:counts.stage,create_calls:counts.create,wait_calls:counts.wait,unexpected_calls:counts.unexpected,same_wait_path:true}));
`

func newRemoteProviderFixture(t *testing.T, label string) remoteProviderFixture {
	t.Helper()
	authoritativeRoot := filepath.Join(t.TempDir(), "authoritative")
	devices, err := edge.Open(edge.Config{Root: filepath.Join(authoritativeRoot, "edge")})
	if err != nil {
		t.Fatal(stageTransportBuild)
	}
	t.Cleanup(func() { _ = devices.Close() })
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(authoritativeRoot, "model-turns")})
	if err != nil {
		t.Fatal(stageTransportBuild)
	}
	t.Cleanup(func() { _ = turns.Close() })
	audit := &remoteRequestAudit{next: edge.NewHTTPHandler(devices, turns), counts: make(map[string]int), active: make(map[string]int), nonces: make(map[string]struct{}), valid: true}
	server := httptest.NewTLSServer(audit)
	t.Cleanup(server.Close)
	caPath := filepath.Join(t.TempDir(), "relay-ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caPath, certificate, 0o644); err != nil {
		t.Fatal(stageTransportBuild)
	}
	edgeState := filepath.Join(t.TempDir(), "edge-state")
	pairingCode, err := devices.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(stageLeaseBinding)
	}
	identity, err := edgeclient.Pair(t.Context(), edgeclient.PairOptions{ServerURL: server.URL, Code: pairingCode, Name: "provider-remote", StateRoot: edgeState, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(stageLeaseBinding)
	}
	audit.setDevice(identity.DeviceID)
	workspaceID := "ws_0123456789abcdef0123456789abcdef"
	goal := []byte("remote provider " + label)
	goalBody, err := turns.StageRuntimeGoal(t.Context(), goal, 5*time.Minute)
	if err != nil {
		t.Fatal(stageLeaseBinding)
	}
	runtimeState, _, err := turns.StartBoundRuntime(t.Context(), modelturn.BoundRuntimeRequest{
		DeviceID: identity.DeviceID, WorkspaceID: workspaceID, Controller: modelturn.ControllerRemoteEdge,
		GoalSummary: modelturn.GoalSummary(goal), GoalRef: goalBody.BodyRef, GoalDigest: goalBody.ContentDigest,
		IdempotencyKeyDigest: modelturn.IdempotencyDigest("provider-remote-" + label), TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(stageLeaseBinding)
	}
	signed, err := edgeclient.NewTransport(edgeState, server.Client())
	if err != nil {
		t.Fatal(stageTransportBuild)
	}
	lease, err := signed.LeaseModelRuntime(t.Context(), time.Second)
	if err != nil || lease == nil || lease.RuntimeID != runtimeState.RuntimeID || lease.DeviceID != identity.DeviceID || lease.WorkspaceID != workspaceID {
		t.Fatal(stageLeaseBinding)
	}
	probe, err := edgeclient.NewRemoteEdgeTransport(edgeclient.RemoteEdgeTransportOptions{StateRoot: edgeState, Lease: *lease, HTTPClient: server.Client(), LongPoll: time.Second})
	if err != nil {
		t.Fatal(stageTransportBuild)
	}
	started, err := probe.Started(t.Context())
	if err != nil || started.RuntimeID != runtimeState.RuntimeID || started.DeviceID != identity.DeviceID || started.WorkspaceID != workspaceID || started.State != modelturn.RuntimeStateAwaitingModel {
		_ = probe.Close()
		t.Fatal(stageRuntimeStatus)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(stageTransportBuild)
	}
	return remoteProviderFixture{turns: turns, server: server, audit: audit, edgeState: edgeState, caPath: caPath, lease: *lease, runtime: runtimeState, workspaceID: workspaceID}
}

func startRemoteProviderDriver(t *testing.T, fixture remoteProviderFixture) *remoteDriverProcess {
	t.Helper()
	payload, err := json.Marshal(fixture.lease)
	if err != nil {
		t.Fatal(stageLeaseBinding)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	process := &remoteDriverProcess{done: make(chan error, 1), cancel: cancel}
	process.command = exec.CommandContext(ctx, providerDriverBin, "--remote", "--state-root", fixture.edgeState, "--socket", filepath.Join(fixture.edgeState, "provider.sock"))
	process.command.Stdin = bytes.NewReader(payload)
	process.command.Stdout = ioDiscard{}
	process.command.Stderr = ioDiscard{}
	process.command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + fixture.edgeState,
		"USER=mcpedge",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"SSL_CERT_FILE=" + fixture.caPath,
	}
	if err := process.command.Start(); err != nil {
		cancel()
		t.Fatal(stageSocketConnect)
	}
	go func() { process.done <- process.command.Wait() }()
	return process
}

func stopRemoteProviderDriver(t *testing.T, process *remoteDriverProcess) {
	t.Helper()
	if process == nil {
		return
	}
	if process.command.Process != nil {
		_ = process.command.Process.Signal(os.Interrupt)
	}
	select {
	case err := <-process.done:
		process.cancel()
		if err != nil && !isExpectedProcessStop(err) {
			t.Fatal(stageSocketConnect)
		}
	case <-time.After(3 * time.Second):
		process.cancel()
		select {
		case <-process.done:
		case <-time.After(2 * time.Second):
			t.Fatal(stageSocketConnect)
		}
	}
}

func startRemoteNodeProvider(t *testing.T, fixture remoteProviderFixture, large bool, expected string) *exec.Cmd {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(stageSocketConnect)
	}
	scriptPath := filepath.Join(t.TempDir(), "provider-remote.mjs")
	if err := os.WriteFile(scriptPath, []byte(remoteProviderScript), 0o600); err != nil {
		t.Fatal(stageTransportBuild)
	}
	providerPath := filepath.Join(providerRepoRoot, "integrations", "opencode", "provider", "index.js")
	mode := "inline"
	if large {
		mode = "request_ref"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, node, scriptPath, providerPath, filepath.Join(fixture.edgeState, "provider.sock"), fixture.runtime.RuntimeID, mode, expected)
	command.Dir = filepath.Dir(providerPath)
	command.Stdout = &bytes.Buffer{}
	command.Stderr = ioDiscard{}
	if err := command.Start(); err != nil {
		t.Fatal(stageSocketConnect)
	}
	return command
}

func waitRemoteNodeProvider(t *testing.T, command *exec.Cmd) remoteProviderResult {
	t.Helper()
	if err := command.Wait(); err != nil {
		t.Fatal(stageResponseWait)
	}
	buffer, ok := command.Stdout.(*bytes.Buffer)
	if !ok {
		t.Fatal(stageResponseIdentity)
	}
	var result remoteProviderResult
	decoder := json.NewDecoder(bytes.NewReader(buffer.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(stageResponseIdentity)
	}
	return result
}

func nextRemoteOffer(t *testing.T, turns *modelturn.Store, runtimeID string) modelturn.Offer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	offer, err := turns.Next(ctx, runtimeID)
	if err != nil {
		t.Fatal(stageTurnCreate)
	}
	return offer
}

func respondRemoteOffer(t *testing.T, turns *modelturn.Store, offer modelturn.Offer, text string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"finish_reason": "stop", "text": text, "tool_calls": []any{}, "usage": nil})
	if err != nil {
		t.Fatal(stageResponseIdentity)
	}
	if _, err := turns.Respond(t.Context(), modelturn.ResponseSubmission{
		RuntimeID: offer.RuntimeID, TurnID: offer.TurnID, ExpectedSequence: offer.Sequence,
		RequestDigest: offer.RequestDigest, Payload: payload,
	}); err != nil {
		t.Fatal(stageResponseWait)
	}
}

func readRemoteJournal(t *testing.T, stateRoot string) (journalTurn, int, int, int) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(stateRoot, "model-relay.db"))
	if err != nil {
		t.Fatal(stageLeaseBinding)
	}
	defer db.Close()
	var entry journalTurn
	if err := db.QueryRow(`SELECT runtime_id,sequence,request_digest,create_id,turn_id,request_ref,wait_id,state FROM remote_model_turns`).Scan(
		&entry.RuntimeID, &entry.Sequence, &entry.RequestDigest, &entry.CreateID, &entry.TurnID, &entry.RequestRef, &entry.WaitID, &entry.State,
	); err != nil {
		t.Fatal(stageResponseIdentity)
	}
	var stagedBodies int
	if err := db.QueryRow(`SELECT COUNT(*) FROM staged_model_bodies`).Scan(&stagedBodies); err != nil {
		t.Fatal(stageRequestStage)
	}
	var forbiddenTables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('model_turns','turn_bodies','model_runtimes')`).Scan(&forbiddenTables); err != nil {
		t.Fatal(stageRequestStage)
	}
	var leaseCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM remote_model_leases`).Scan(&leaseCount); err != nil {
		t.Fatal(stageLeaseBinding)
	}
	return entry, stagedBodies, forbiddenTables, leaseCount
}

func assertRemoteStoreConsumedOnce(t *testing.T, turns *modelturn.Store, offer modelturn.Offer) {
	t.Helper()
	stats, err := turns.Stats(t.Context())
	if err != nil || stats.RuntimeCount != 1 || stats.TurnCount != 1 || stats.ConsumedCount != 1 || stats.AwaitingCount != 0 || stats.RespondedCount != 0 {
		t.Fatal(stageResponseConsume)
	}
	record, err := turns.Get(t.Context(), offer.TurnID)
	if err != nil || record.RuntimeID != offer.RuntimeID || record.TurnID != offer.TurnID || record.Sequence != offer.Sequence || record.RequestDigest != offer.RequestDigest || record.RequestRef != offer.RequestRef || record.Status != modelturn.StatusConsumed {
		t.Fatal(stageResponseIdentity)
	}
	if _, err := turns.WaitResponse(t.Context(), offer.TurnID); !errors.Is(err, modelturn.ErrResponseReplay) {
		t.Fatal(stageResponseConsume)
	}
}

func waitForProviderDriverWait(t *testing.T, socketPath string, audit *remoteRequestAudit) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		metrics, err := readProviderDriverMetrics(socketPath)
		if err == nil && metrics.WaitCalls > 0 && audit.activeCount(stageResponseWait) == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(stageResponseWait)
}

func waitForRemoteWaitRelease(t *testing.T, audit *remoteRequestAudit) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if audit.activeCount(stageResponseWait) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(stageResponseWait)
}

func closedResultStage(status string) string {
	switch status {
	case stageTransportBuild, stageLeaseBinding, stageRuntimeStatus, stageSocketConnect, stageRequestStage, stageTurnCreate, stageResponseWait, stageResponseIdentity, stageResponseConsume:
		return status
	default:
		return stageResponseIdentity
	}
}

func isExpectedProcessStop(err error) bool {
	var exitError *exec.ExitError
	return errors.As(err, &exitError)
}

type ioDiscard struct{}

func (ioDiscard) Write(payload []byte) (int, error) { return len(payload), nil }

func (a *remoteRequestAudit) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	kind := remoteRequestKind(request.Method, request.URL.Path)
	if kind != "" {
		a.mu.Lock()
		a.counts[kind]++
		if kind == stageResponseWait {
			a.active[kind]++
			defer func() {
				a.mu.Lock()
				a.active[kind]--
				a.mu.Unlock()
			}()
		}
		deviceID := request.Header.Get(edge.HeaderDevice)
		timestamp := request.Header.Get(edge.HeaderTimestamp)
		nonce := request.Header.Get(edge.HeaderNonce)
		signature := request.Header.Get(edge.HeaderSignature)
		parsedTimestamp, timestampErr := strconv.ParseInt(timestamp, 10, 64)
		_, reused := a.nonces[nonce]
		if nonce != "" {
			a.nonces[nonce] = struct{}{}
		}
		if a.deviceID == "" || deviceID != a.deviceID || timestampErr != nil || time.Since(time.Unix(parsedTimestamp, 0)).Abs() > edge.SignatureWindow || nonce == "" || signature == "" || reused {
			a.valid = false
		}
		a.mu.Unlock()
	}
	a.next.ServeHTTP(writer, request)
}

func (a *remoteRequestAudit) setDevice(deviceID string) {
	a.mu.Lock()
	a.deviceID = deviceID
	a.mu.Unlock()
}

func (a *remoteRequestAudit) count(kind string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.counts[kind]
}

func (a *remoteRequestAudit) activeCount(kind string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.active[kind]
}

func (a *remoteRequestAudit) ok() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.valid
}

func remoteRequestKind(method, path string) string {
	switch {
	case method == http.MethodPost && path == "/edge/v1/model-runtimes/lease":
		return stageLeaseBinding
	case method == http.MethodPost && strings.HasSuffix(path, "/started"):
		return stageRuntimeStatus
	case method == http.MethodPost && strings.HasSuffix(path, "/turns"):
		return stageTurnCreate
	case method == http.MethodPost && strings.HasSuffix(path, "/wait"):
		return stageResponseWait
	default:
		return ""
	}
}

const remoteProviderScript = `
import { pathToFileURL } from "node:url";
const { createMCPDevboxModelBridge, __test } = await import(pathToFileURL(process.argv[2]).href);
const socketPath = process.argv[3];
const runtimeID = process.argv[4];
const mode = process.argv[5];
const expected = process.argv[6];
const base = __test.createUnixRequester(socketPath);
const counts = {runtime: 0, stage: 0, create: 0, wait: 0, unexpected: 0};
const waitPaths = [];
let activeStage = "runtime_status";
function stageFor(request) {
  if (request.method === "GET" && request.path === "/v1/runtimes/" + runtimeID) return "runtime_status";
  if (request.method === "POST" && request.path === "/v1/request-bodies") return "request_stage";
  if (request.method === "POST" && request.path === "/v1/turns") return "turn_create";
  if (request.method === "GET" && request.path.endsWith("/response")) return "response_wait";
  return "response_identity";
}
const requestImpl = async (request) => {
  const stage = stageFor(request);
  activeStage = stage;
  if (stage === "runtime_status") counts.runtime += 1;
  else if (stage === "request_stage") counts.stage += 1;
  else if (stage === "turn_create") counts.create += 1;
  else if (stage === "response_wait") { counts.wait += 1; waitPaths.push(request.path); }
  else counts.unexpected += 1;
  try {
    return await base(request);
  } catch (error) {
    error.closedStage = stage;
    throw error;
  }
};
function closedCode(error) {
  if (error?.closedStage) return error.closedStage;
  const message = String(error?.message ?? "").toLowerCase();
  if (message.includes("runtime")) return "runtime_status";
  if (message.includes("staged") || message.includes("request reference")) return "request_stage";
  if (message.includes("created turn")) return "turn_create";
  if (message.includes("response") || message.includes("finish reason") || message.includes("usage")) return "response_identity";
  return activeStage;
}
let status = "ok";
try {
  const text = mode === "request_ref" ? "x".repeat((64 << 10) + 4096) : "inline";
  const model = createMCPDevboxModelBridge({socketPath, runtimeID, requestImpl, ttlMs: 60000, timeoutMs: 15000}).languageModel("external-model");
  const result = await model.doGenerate({prompt: [{role: "user", content: [{type: "text", text}]}], tools: []});
  if (result.content.length !== 1 || result.content[0].type !== "text" || result.content[0].text !== expected) status = "response_identity";
} catch (error) {
  status = closedCode(error);
}
const expectedWaitPath = waitPaths.length > 0 ? waitPaths[0] : "";
process.stdout.write(JSON.stringify({
  status,
  runtime_calls: counts.runtime,
  stage_calls: counts.stage,
  create_calls: counts.create,
  wait_calls: counts.wait,
  unexpected_calls: counts.unexpected,
  same_wait_path: waitPaths.length > 0 && waitPaths.every((path) => path === expectedWaitPath),
}));
`
