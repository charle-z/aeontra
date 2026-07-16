//go:build opencode_e2e

package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

type remoteDriverProcess struct {
	cmd    *exec.Cmd
	done   chan error
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func TestRemoteModelTurnDriverRestartResumesExactTurn(t *testing.T) {
	driverBinary := requiredAbsoluteFile(t, "MODEL_TURN_DRIVER_E2E_BIN")
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
	httpServer := httptest.NewTLSServer(edge.NewHTTPHandler(devices, turns))
	defer httpServer.Close()
	caPath := filepath.Join(t.TempDir(), "relay-ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: httpServer.Certificate().Raw}), 0o644); err != nil {
		t.Fatal(err)
	}

	edgeState := filepath.Join(t.TempDir(), "edge-state")
	pairingCode, err := devices.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := edgeclient.Pair(t.Context(), edgeclient.PairOptions{
		ServerURL: httpServer.URL, Code: pairingCode, Name: "driver-restart-e2e", StateRoot: edgeState, HTTPClient: httpServer.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	workspacePath := createCalcRepo(t, "driver-restart")
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
	server, _, _ := e2eServer(t, authoritativeRoot, turns)
	server.WithEdgeStore(devices)
	meter := &e2eMeter{}
	startText := mcpTool(t, server, meter, "opencode_runtime_start", map[string]any{
		"device_id": identity.DeviceID, "workspace_id": workspace.ID,
		"goal":            "Validate driver restart without duplicating the model turn.",
		"timeout_seconds": 120, "idempotency_key": "remote-driver-restart-e2e",
	})
	var public runtimePublicView
	decodeToolJSON(t, startText, &public)
	transport, err := edgeclient.NewTransport(edgeState, httpServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	lease, err := transport.LeaseModelRuntime(t.Context(), 3*time.Second)
	if err != nil || lease == nil || lease.RuntimeID != public.RuntimeID || lease.WorkspaceID != workspace.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	lifecycle, err := edgeclient.NewRemoteEdgeTransport(edgeclient.RemoteEdgeTransportOptions{StateRoot: edgeState, Lease: *lease, HTTPClient: httpServer.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Started(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Close(); err != nil {
		t.Fatal(err)
	}

	socketRoot := filepath.Join(edgeState, "driver-restart")
	if err := os.MkdirAll(socketRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(socketRoot, "d.sock")
	first := startRemoteDriverProcess(t, driverBinary, edgeState, socketPath, caPath, *lease)
	client := unixDriverClient(socketPath)
	waitDriverHealth(t, client)
	payload := json.RawMessage(`{"messages":[{"content":"resume","role":"user"}],"tools":[]}`)
	digest, err := modelturn.ExactPayloadDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	createBody, _ := json.Marshal(map[string]any{
		"runtime_id": lease.RuntimeID, "sequence": 1, "request_digest": digest,
		"payload": payload, "ttl_ms": int64(time.Minute / time.Millisecond),
	})
	createdTurn := postDriverTurn(t, client, createBody)
	before, err := turns.Get(t.Context(), createdTurn.ID)
	if err != nil {
		t.Fatal(err)
	}
	stopRemoteDriverProcess(t, first)
	connection, dialErr := net.DialTimeout("unix", socketPath, 250*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		t.Fatal("stopped driver socket still accepted connections")
	}

	if _, err := turns.Respond(t.Context(), modelturn.ResponseSubmission{
		RuntimeID: lease.RuntimeID, TurnID: createdTurn.ID, ExpectedSequence: createdTurn.Sequence,
		RequestDigest: digest, Payload: json.RawMessage(`{"finish_reason":"stop","text":"resumed exactly once"}`),
	}); err != nil {
		t.Fatal(err)
	}
	second := startRemoteDriverProcess(t, driverBinary, edgeState, socketPath, caPath, *lease)
	defer stopRemoteDriverProcess(t, second)
	client = unixDriverClient(socketPath)
	waitDriverHealth(t, client)
	replayedCreate := postDriverTurn(t, client, createBody)
	if replayedCreate.ID != createdTurn.ID || replayedCreate.RuntimeID != createdTurn.RuntimeID || replayedCreate.Sequence != createdTurn.Sequence || replayedCreate.RequestDigest != createdTurn.RequestDigest || replayedCreate.RequestRef != createdTurn.RequestRef {
		t.Fatalf("replayed create changed identity: before=%+v after=%+v", createdTurn, replayedCreate)
	}
	response := getDriverResponse(t, client, createdTurn.ID)
	var responsePayload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(response.Payload, &responsePayload); err != nil {
		t.Fatal(err)
	}
	if responsePayload.Text != "resumed exactly once" || response.RequestDigest != digest || response.TurnID != createdTurn.ID || response.RuntimeID != lease.RuntimeID {
		t.Fatalf("response=%+v payload=%+v", response, responsePayload)
	}
	replayedResponse := getDriverResponse(t, client, createdTurn.ID)
	if !bytes.Equal(replayedResponse.Payload, response.Payload) || replayedResponse.TurnID != response.TurnID || replayedResponse.RequestDigest != response.RequestDigest {
		t.Fatalf("replayed response changed: first=%+v second=%+v", response, replayedResponse)
	}
	after, err := turns.Get(t.Context(), createdTurn.ID)
	if err != nil || after.Status != modelturn.StatusConsumed || after.TurnID != before.TurnID || after.Sequence != before.Sequence || after.RequestDigest != before.RequestDigest || after.RequestRef != before.RequestRef {
		t.Fatalf("after=%+v before=%+v err=%v", after, before, err)
	}
	stats, err := turns.Stats(t.Context())
	if err != nil || stats.RuntimeCount != 1 || stats.TurnCount != 1 || stats.ConsumedCount != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	journalInfo, err := os.Stat(filepath.Join(edgeState, "model-relay.db"))
	if err != nil || journalInfo.Mode().Perm() != 0o600 {
		var mode os.FileMode
		if journalInfo != nil {
			mode = journalInfo.Mode().Perm()
		}
		t.Fatalf("journal mode=%v err=%v", mode, err)
	}
	journalBody, err := os.ReadFile(filepath.Join(edgeState, "model-relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{workspacePath, httpServer.URL, "resumed exactly once", "Validate driver restart"} {
		if strings.Contains(string(journalBody), forbidden) {
			t.Fatalf("Edge journal contains forbidden data %q", forbidden)
		}
	}
	if meter.Calls != 1 {
		t.Fatalf("unexpected MCP/control calls=%d", meter.Calls)
	}
}

func startRemoteDriverProcess(t *testing.T, binary, stateRoot, socketPath, caPath string, lease edgeclient.ModelRuntimeLease) *remoteDriverProcess {
	t.Helper()
	payload, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	process := &remoteDriverProcess{done: make(chan error, 1)}
	process.cmd = exec.CommandContext(ctx, binary, "--remote", "--state-root", stateRoot, "--socket", socketPath)
	process.cmd.Stdin = bytes.NewReader(payload)
	process.cmd.Stdout = &process.stdout
	process.cmd.Stderr = &process.stderr
	process.cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + stateRoot, "USER=mcpedge", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "SSL_CERT_FILE=" + caPath,
	}
	if err := process.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { process.done <- process.cmd.Wait() }()
	return process
}

func stopRemoteDriverProcess(t *testing.T, process *remoteDriverProcess) {
	t.Helper()
	if process == nil || process.cmd == nil || process.cmd.Process == nil {
		return
	}
	_ = process.cmd.Process.Kill()
	select {
	case <-process.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("driver did not stop stdout=%s stderr=%s", process.stdout.String(), process.stderr.String())
	}
}

func unixDriverClient(socketPath string) *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}},
	}
}

func waitDriverHealth(t *testing.T, client *http.Client) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get("http://unix/healthz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("driver health endpoint did not become ready")
}

func postDriverTurn(t *testing.T, client *http.Client, body []byte) modelturn.Turn {
	t.Helper()
	response, err := client.Post("http://unix/v1/turns", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	encoded, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d body=%s", response.StatusCode, encoded)
	}
	var turn modelturn.Turn
	if err := json.Unmarshal(encoded, &turn); err != nil {
		t.Fatal(err)
	}
	return turn
}

func getDriverResponse(t *testing.T, client *http.Client, turnID modelturn.TurnID) modelturn.ModelResponse {
	t.Helper()
	response, err := client.Get("http://unix/v1/turns/" + string(turnID) + "/response")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	encoded, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("wait status=%d body=%s", response.StatusCode, encoded)
	}
	var modelResponse modelturn.ModelResponse
	if err := json.Unmarshal(encoded, &modelResponse); err != nil {
		t.Fatal(err)
	}
	return modelResponse
}
