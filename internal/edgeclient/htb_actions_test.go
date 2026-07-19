//go:build !windows

package edgeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type countingHTBProbe struct {
	interfaceCalls atomic.Int32
	routeCalls     atomic.Int32
	failRoute      bool
}

func (probe *countingHTBProbe) InterfaceIPv4(context.Context, string) (string, error) {
	probe.interfaceCalls.Add(1)
	return "10.10.15.152", nil
}

func (probe *countingHTBProbe) RouteInterface(context.Context, string) (string, error) {
	probe.routeCalls.Add(1)
	if probe.failRoute {
		return "eth0", nil
	}
	return "tun0", nil
}

type htbSSHObservation struct {
	calls             int
	credentialInStdin bool
	secretInArgs      bool
	secretInEnv       bool
}

func installStructuredHTBSSHFixture(t *testing.T, broker *htbLabBroker, workspace, secret string) *htbSSHObservation {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workspace, "loot", "capture.txt"), []byte("USER nathan\nPASS "+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	observation := &htbSSHObservation{}
	oldResolve := htbLabResolveSSH
	oldSelf := htbLabSelfExecutable
	oldRun := htbLabRunSSHProcess
	htbLabResolveSSH = func(string, string) (string, bool) { return "/usr/bin/ssh", true }
	htbLabSelfExecutable = func() (string, error) { return "/usr/local/bin/mcp-edge", nil }
	htbLabRunSSHProcess = func(_ context.Context, _ string, args, environment []string, stdin []byte, stdout, stderr io.Writer) error {
		observation.calls++
		joinedArgs := strings.Join(args, " ")
		joinedEnv := strings.Join(environment, "\n")
		observation.secretInArgs = observation.secretInArgs || strings.Contains(joinedArgs, secret)
		observation.secretInEnv = observation.secretInEnv || strings.Contains(joinedEnv, secret)
		if !strings.Contains(joinedArgs, "nathan@10.129.63.65") || strings.Contains(joinedArgs, "10.129.59.198") {
			t.Fatalf("target locking failed: %q", args)
		}
		askpass := ""
		for _, value := range environment {
			if strings.HasPrefix(value, "MCP_DEVBOX_ASKPASS_FILE=") {
				askpass = strings.TrimPrefix(value, "MCP_DEVBOX_ASKPASS_FILE=")
			}
		}
		body, err := os.ReadFile(askpass)
		if err != nil || string(body) != secret {
			t.Fatalf("askpass=%q err=%v", body, err)
		}
		command := args[len(args)-1]
		switch command {
		case "id -u && id -g && id -un":
			_, _ = io.WriteString(stdout, "1000\n1000\nnathan\n")
		case "id":
			_, _ = io.WriteString(stdout, "uid=1000(nathan) gid=1000(nathan)\n")
		case "emit-sensitive":
			_, _ = io.WriteString(stdout, secret+"\n0123456789abcdef0123456789abcdef\n")
		case "cat /home/nathan/user.txt":
			_, _ = io.WriteString(stdout, "0123456789abcdef0123456789abcdef\n")
		case "sudo -S -l":
			observation.credentialInStdin = string(stdin) == secret+"\n"
			_, _ = io.WriteString(stdout, "sudo fixture ok\n")
		case "huge-stderr":
			_, _ = stderr.Write(bytes.Repeat([]byte("x"), (256<<10)+1024))
		default:
			_, _ = io.WriteString(stderr, "fixture command not recognized")
		}
		return nil
	}
	t.Cleanup(func() {
		htbLabResolveSSH = oldResolve
		htbLabSelfExecutable = oldSelf
		htbLabRunSSHProcess = oldRun
	})
	return observation
}

func postHTBAction(t *testing.T, handler http.HandlerFunc, value any, output any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://unix", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	if output != nil {
		if err := json.Unmarshal(recorder.Body.Bytes(), output); err != nil {
			t.Fatalf("status=%d body=%q err=%v", recorder.Code, recorder.Body.String(), err)
		}
	}
	return recorder
}

func createHTBSession(t *testing.T, broker *htbLabBroker) HTBAuthValidateResponse {
	t.Helper()
	var response HTBAuthValidateResponse
	recorder := postHTBAction(t, broker.authValidate, HTBAuthValidateRequest{
		WorkspaceID: broker.config.Workspace.ID, Username: "nathan",
		Credential: HTBCredentialReference{Source: "loot/capture.txt", ExtractAfter: "PASS"}, TimeoutSeconds: 30,
	}, &response)
	if recorder.Code != http.StatusOK || response.Status != "ok" || !htbLabSessionPattern.MatchString(response.SessionID) {
		t.Fatalf("status=%d response=%+v", recorder.Code, response)
	}
	return response
}

func TestStructuredHTBSessionKeepsCredentialLocalAndRedactsSensitiveOutput(t *testing.T) {
	broker, workspace := newHTBLabBrokerFixture(t)
	probe := &countingHTBProbe{}
	broker.config.Probe = probe
	const secret = "local-fixture-password"
	observation := installStructuredHTBSSHFixture(t, broker, workspace, secret)
	session := createHTBSession(t, broker)

	var response HTBCommandResponse
	recorder := postHTBAction(t, broker.command, HTBSessionCommandRequest{
		WorkspaceID: broker.config.Workspace.ID, SessionID: session.SessionID, Command: "emit-sensitive", TimeoutSeconds: 30,
	}, &response)
	if recorder.Code != http.StatusOK || response.Status != "ok" {
		t.Fatalf("status=%d response=%+v", recorder.Code, response)
	}
	encoded := recorder.Body.String()
	for _, forbidden := range []string{secret, "0123456789abcdef0123456789abcdef", broker.config.Workspace.TargetIP} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(response.Stdout, "[REDACTED-CREDENTIAL]") || !strings.Contains(response.Stdout, "[REDACTED-HTB-FLAG]") {
		t.Fatalf("redaction missing: %+v", response)
	}
	if observation.secretInArgs || observation.secretInEnv || observation.calls != 2 {
		t.Fatalf("observation=%+v", observation)
	}
	if probe.interfaceCalls.Load() != 2 || probe.routeCalls.Load() != 2 {
		t.Fatalf("VPN was not revalidated per operation: interface=%d route=%d", probe.interfaceCalls.Load(), probe.routeCalls.Load())
	}
	entries, err := os.ReadDir(filepath.Join(broker.config.StateRoot, "lab-secrets"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary secret files=%v err=%v", entries, err)
	}
}

func TestStructuredHTBCommandSaveReturnsOnlyMetadataAndCredentialStdinStaysOffArgv(t *testing.T) {
	broker, workspace := newHTBLabBrokerFixture(t)
	const secret = "local-fixture-password"
	observation := installStructuredHTBSSHFixture(t, broker, workspace, secret)
	session := createHTBSession(t, broker)

	var saved HTBCommandSaveResponse
	recorder := postHTBAction(t, broker.commandSave, HTBSessionCommandSaveRequest{
		WorkspaceID: broker.config.Workspace.ID, SessionID: session.SessionID,
		Command: "cat /home/nathan/user.txt", SaveOutput: "loot/user.txt", TimeoutSeconds: 30,
	}, &saved)
	if recorder.Code != http.StatusOK || saved.Path != "loot/user.txt" || saved.Bytes != 33 || len(saved.SHA256) != 64 || !saved.NonEmpty {
		t.Fatalf("status=%d saved=%+v", recorder.Code, saved)
	}
	if strings.Contains(recorder.Body.String(), "0123456789abcdef0123456789abcdef") {
		t.Fatalf("saved output leaked: %s", recorder.Body.String())
	}

	var sudo HTBCommandResponse
	recorder = postHTBAction(t, broker.commandCredentialStdin, HTBSessionCommandRequest{
		WorkspaceID: broker.config.Workspace.ID, SessionID: session.SessionID, Command: "sudo -S -l", TimeoutSeconds: 30,
	}, &sudo)
	if recorder.Code != http.StatusOK || !observation.credentialInStdin || observation.secretInArgs || observation.secretInEnv {
		t.Fatalf("status=%d observation=%+v sudo=%+v", recorder.Code, observation, sudo)
	}
}

func TestStructuredHTBSessionsAreRuntimeBoundExpireAndClose(t *testing.T) {
	broker, workspace := newHTBLabBrokerFixture(t)
	installStructuredHTBSSHFixture(t, broker, workspace, "local-fixture-password")
	session := createHTBSession(t, broker)

	broker.mu.Lock()
	stored := broker.sessions[session.SessionID]
	stored.RuntimeID = "mr_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	broker.sessions[session.SessionID] = stored
	broker.mu.Unlock()
	var failure map[string]any
	recorder := postHTBAction(t, broker.command, HTBSessionCommandRequest{
		WorkspaceID: broker.config.Workspace.ID, SessionID: session.SessionID, Command: "id", TimeoutSeconds: 30,
	}, &failure)
	if recorder.Code != http.StatusBadGateway || failure["failure_category"] != "session_not_found" {
		t.Fatalf("runtime mismatch accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	session = createHTBSession(t, broker)
	broker.mu.Lock()
	stored = broker.sessions[session.SessionID]
	stored.ExpiresAt = time.Now().UTC().Add(-time.Second)
	broker.sessions[session.SessionID] = stored
	broker.mu.Unlock()
	recorder = postHTBAction(t, broker.command, HTBSessionCommandRequest{
		WorkspaceID: broker.config.Workspace.ID, SessionID: session.SessionID, Command: "id", TimeoutSeconds: 30,
	}, &failure)
	if recorder.Code != http.StatusBadGateway || failure["failure_category"] != "session_expired" {
		t.Fatalf("expired session accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	session = createHTBSession(t, broker)
	var closed HTBSessionCloseResponse
	recorder = postHTBAction(t, broker.sessionClose, HTBSessionCloseRequest{WorkspaceID: broker.config.Workspace.ID, SessionID: session.SessionID}, &closed)
	if recorder.Code != http.StatusOK || !closed.Closed {
		t.Fatalf("close failed: status=%d response=%+v", recorder.Code, closed)
	}
	recorder = postHTBAction(t, broker.command, HTBSessionCommandRequest{
		WorkspaceID: broker.config.Workspace.ID, SessionID: session.SessionID, Command: "id", TimeoutSeconds: 30,
	}, &failure)
	if recorder.Code != http.StatusBadGateway || failure["failure_category"] != "session_not_found" {
		t.Fatalf("closed session reused: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestStructuredHTBActionsRejectCallerSelectedTargetPasswordAndUnknownFields(t *testing.T) {
	broker, _ := newHTBLabBrokerFixture(t)
	for _, body := range []string{
		`{"workspace_id":"` + broker.config.Workspace.ID + `","target":"10.129.59.198"}`,
		`{"workspace_id":"` + broker.config.Workspace.ID + `","password":"secret"}`,
		`{"workspace_id":"` + broker.config.Workspace.ID + `","host":"other"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "http://unix", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		broker.status(recorder, request)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "invalid_request") {
			t.Fatalf("unexpected field accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestStructuredHTBEndToEndReadsHandleRunsIDStoresArtifactAndClosesWithoutSecretLeak(t *testing.T) {
	broker, workspace := newHTBLabBrokerFixture(t)
	const secret = "local-e2e-password"
	installStructuredHTBSSHFixture(t, broker, workspace, secret)
	var transcript strings.Builder

	session := createHTBSession(t, broker)
	encodedSession, _ := json.Marshal(session)
	transcript.Write(encodedSession)

	var identity HTBCommandResponse
	recorder := postHTBAction(t, broker.command, HTBSessionCommandRequest{
		WorkspaceID: broker.config.Workspace.ID, SessionID: session.SessionID, Command: "id", TimeoutSeconds: 30,
	}, &identity)
	if recorder.Code != http.StatusOK || !strings.Contains(identity.Stdout, "uid=1000") {
		t.Fatalf("identity status=%d response=%+v", recorder.Code, identity)
	}
	transcript.Write(recorder.Body.Bytes())

	var saved HTBCommandSaveResponse
	recorder = postHTBAction(t, broker.commandSave, HTBSessionCommandSaveRequest{
		WorkspaceID: broker.config.Workspace.ID, SessionID: session.SessionID,
		Command: "cat /home/nathan/user.txt", SaveOutput: "loot/user.txt", TimeoutSeconds: 30,
	}, &saved)
	if recorder.Code != http.StatusOK || saved.Path != "loot/user.txt" || !saved.NonEmpty {
		t.Fatalf("save status=%d response=%+v", recorder.Code, saved)
	}
	transcript.Write(recorder.Body.Bytes())

	var status HTBWorkspaceStatusResponse
	recorder = postHTBAction(t, broker.status, HTBWorkspaceRequest{WorkspaceID: broker.config.Workspace.ID}, &status)
	if recorder.Code != http.StatusOK || !status.UserArtifact.NonEmpty || status.NextPhase != "enumerate_privileges" {
		t.Fatalf("status=%d response=%+v", recorder.Code, status)
	}
	transcript.Write(recorder.Body.Bytes())

	var closed HTBSessionCloseResponse
	recorder = postHTBAction(t, broker.sessionClose, HTBSessionCloseRequest{WorkspaceID: broker.config.Workspace.ID, SessionID: session.SessionID}, &closed)
	if recorder.Code != http.StatusOK || !closed.Closed {
		t.Fatalf("close status=%d response=%+v", recorder.Code, closed)
	}
	transcript.Write(recorder.Body.Bytes())

	text := transcript.String()
	for _, forbidden := range []string{secret, "0123456789abcdef0123456789abcdef", broker.config.Workspace.TargetIP} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("structured E2E leaked %q: %s", forbidden, text)
		}
	}
}

func TestClassifyHTBLabSSHProcessResultPreservesRemoteExitCodes(t *testing.T) {
	exitSeven := exec.Command("/bin/sh", "-c", "exit 7").Run()
	code, err := classifyHTBLabSSHProcessResult(context.Background(), exitSeven)
	if err != nil || code != 7 {
		t.Fatalf("remote exit code=%d err=%v", code, err)
	}
	exitTransport := exec.Command("/bin/sh", "-c", "exit 255").Run()
	if code, err := classifyHTBLabSSHProcessResult(context.Background(), exitTransport); err == nil || code != 0 {
		t.Fatalf("transport exit accepted: code=%d err=%v", code, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if code, err := classifyHTBLabSSHProcessResult(cancelled, exitSeven); err == nil || code != 0 {
		t.Fatalf("cancelled command accepted: code=%d err=%v", code, err)
	}
}

func TestStructuredHTBOutputIsBoundedAndMarkedTruncated(t *testing.T) {
	broker, workspace := newHTBLabBrokerFixture(t)
	installStructuredHTBSSHFixture(t, broker, workspace, "local-fixture-password")
	session := createHTBSession(t, broker)
	var response HTBCommandResponse
	recorder := postHTBAction(t, broker.command, HTBSessionCommandRequest{
		WorkspaceID: broker.config.Workspace.ID, SessionID: session.SessionID, Command: "huge-stderr", TimeoutSeconds: 30,
	}, &response)
	if recorder.Code != http.StatusOK || !response.Truncated || len(response.Stderr) != 256<<10 {
		t.Fatalf("status=%d truncated=%t stderr=%d", recorder.Code, response.Truncated, len(response.Stderr))
	}
}

func TestStructuredHTBStopClearsRuntimeSessions(t *testing.T) {
	broker, workspace := newHTBLabBrokerFixture(t)
	installStructuredHTBSSHFixture(t, broker, workspace, "local-fixture-password")
	session := createHTBSession(t, broker)
	broker.closeAllSessions()
	if _, err := broker.activeSession(broker.config.Workspace.ID, session.SessionID); err == nil || err.Error() != "session_not_found" {
		t.Fatalf("session survived STOP cleanup: %v", err)
	}
}

func TestStructuredHTBCommandsDoNotConsumeAuthenticationAttemptBudget(t *testing.T) {
	broker, workspace := newHTBLabBrokerFixture(t)
	installStructuredHTBSSHFixture(t, broker, workspace, "local-fixture-password")
	session := createHTBSession(t, broker)
	broker.attempts.Store(64)
	var response HTBCommandResponse
	recorder := postHTBAction(t, broker.command, HTBSessionCommandRequest{
		WorkspaceID: broker.config.Workspace.ID, SessionID: session.SessionID, Command: "id", TimeoutSeconds: 30,
	}, &response)
	if recorder.Code != http.StatusOK || response.Status != "ok" {
		t.Fatalf("validated session command was rate limited: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var failure map[string]any
	recorder = postHTBAction(t, broker.authValidate, HTBAuthValidateRequest{
		WorkspaceID: broker.config.Workspace.ID, Username: "nathan",
		Credential: HTBCredentialReference{Source: "loot/capture.txt", ExtractAfter: "PASS"}, TimeoutSeconds: 30,
	}, &failure)
	if recorder.Code != http.StatusTooManyRequests || failure["failure_category"] != "attempt_limit" {
		t.Fatalf("authentication limit was not enforced: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
