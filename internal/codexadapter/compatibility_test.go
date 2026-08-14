package codexadapter_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/codexadapter"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	codexBinaryEnv = "MCP_DEVBOX_CODEX_BIN"
	spikeMarker    = "MCP_DEVBOX_CODEX_STOCK_OK"
	toolMarker     = "MCP_DEVBOX_CODEX_TOOL_OK"
	loopMarker     = "MCP_DEVBOX_CODEX_LOOP_OK"
)

// TestOfficialCodexScriptedResponsesCompatibility is an explicit host acceptance
// test. Normal CI verifies the immutable pin; a release/Edge acceptance job supplies
// the independently downloaded binary through MCP_DEVBOX_CODEX_BIN.
func TestOfficialCodexScriptedResponsesCompatibility(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("official Linux artifact acceptance requires Linux")
	}
	bin := verifiedCodexBinary(t)
	requestSeen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/responses" {
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Authorization") != "" {
			t.Errorf("scripted provider unexpectedly sent Authorization")
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, 4<<20))
		if err != nil {
			t.Errorf("read Responses request: %v", err)
			http.Error(writer, "request", http.StatusBadRequest)
			return
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode Responses request: %v", err)
			http.Error(writer, "request", http.StatusBadRequest)
			return
		}
		var model string
		if err := json.Unmarshal(payload["model"], &model); err != nil || model != "mcp-devbox-scripted" {
			t.Errorf("unexpected model: %q err=%v", model, err)
		}
		select {
		case requestSeen <- struct{}{}:
		default:
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.Header().Set("Cache-Control", "no-cache")
		_, _ = io.WriteString(writer, scriptedResponsesSSE(spikeMarker))
	}))
	defer server.Close()

	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	if err := os.Mkdir(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	args := []string{
		"exec",
		"--ignore-user-config",
		"--ephemeral",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--cd", root,
		"--config", `model="mcp-devbox-scripted"`,
		"--config", `model_provider="mcp-devbox"`,
		"--config", `model_providers.mcp-devbox.name="MCP Devbox scripted"`,
		"--config", fmt.Sprintf("model_providers.mcp-devbox.base_url=%q", server.URL+"/v1"),
		"--config", `model_providers.mcp-devbox.wire_api="responses"`,
		"--config", `model_providers.mcp-devbox.requires_openai_auth=false`,
		"--config", `model_providers.mcp-devbox.supports_websockets=false`,
		"Return the scripted marker and do not call tools.",
	}
	command := exec.CommandContext(ctx, bin, args...)
	command.Env = scrubModelCredentials(os.Environ(), codexHome)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("stock Codex scripted provider failed: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte(spikeMarker)) {
		t.Fatalf("stock Codex output missing marker:\n%s", output)
	}
	select {
	case <-requestSeen:
	default:
		t.Fatal("stock Codex did not reach the scripted Responses provider")
	}
}

func TestOfficialCodexAppServerInitialize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("official Linux artifact acceptance requires Linux")
	}
	bin := verifiedCodexBinary(t)
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	if err := os.Mkdir(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, bin, "app-server", "--stdio", "--strict-config")
	command.Env = scrubModelCredentials(os.Environ(), codexHome)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		_ = command.Wait()
	}()
	initialize := `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"mcp-devbox-spike","title":"MCP Devbox spike","version":"1.0.0"},"capabilities":{}}}` + "\n"
	if _, err := io.WriteString(stdin, initialize); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read app-server initialize: %v stderr=%s", err, stderr.String())
	}
	var response struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatalf("decode app-server initialize: %v line=%s", err, line)
	}
	if response.ID != 1 || len(response.Result) == 0 || len(response.Error) != 0 {
		t.Fatalf("unexpected app-server initialize response: %s", line)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result["userAgent"]) == 0 || len(result["codexHome"]) == 0 {
		t.Fatalf("app-server response lacks identity fields: %s", response.Result)
	}
}

func TestOfficialCodexResponsesAdapterToolLoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("official Linux artifact acceptance requires Linux")
	}
	bin := verifiedCodexBinary(t)
	transport := newScriptedAdapterTransport()
	adapter, err := codexadapter.New(codexadapter.Options{
		RuntimeID: transport.runtime.RuntimeID,
		ModelID:   "mcp-devbox-scripted",
		Transport: transport,
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(adapter.Handler())
	defer server.Close()

	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	if err := os.Mkdir(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	args := []string{
		"exec",
		"--ignore-user-config",
		"--ephemeral",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--cd", root,
		"--config", `model="mcp-devbox-scripted"`,
		"--config", `model_provider="mcp-devbox"`,
		"--config", `model_providers.mcp-devbox.name="MCP Devbox scripted"`,
		"--config", fmt.Sprintf("model_providers.mcp-devbox.base_url=%q", server.URL+"/v1"),
		"--config", `model_providers.mcp-devbox.wire_api="responses"`,
		"--config", `model_providers.mcp-devbox.requires_openai_auth=false`,
		"--config", `model_providers.mcp-devbox.supports_websockets=false`,
		"Use exec_command once and then return the final scripted marker.",
	}
	command := exec.CommandContext(ctx, bin, args...)
	command.Env = scrubModelCredentials(os.Environ(), codexHome)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("stock Codex adapter tool loop failed: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte(loopMarker)) {
		t.Fatalf("stock Codex output missing loop marker:\n%s", output)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.runtime.LastSequence != 2 || !transport.toolResultSeen {
		t.Fatalf("turns=%d tool_result_seen=%v", transport.runtime.LastSequence, transport.toolResultSeen)
	}
}

type scriptedAdapterTransport struct {
	mu             sync.Mutex
	runtime        modelturn.Runtime
	responses      map[modelturn.TurnID]modelturn.ModelResponse
	toolResultSeen bool
}

func newScriptedAdapterTransport() *scriptedAdapterTransport {
	return &scriptedAdapterTransport{
		runtime: modelturn.Runtime{
			RuntimeID: "mr_22222222222222222222222222222222",
			Status:    modelturn.RuntimeRunning,
		},
		responses: make(map[modelturn.TurnID]modelturn.ModelResponse),
	}
}

func (t *scriptedAdapterTransport) Runtime(context.Context, string) (modelturn.Runtime, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.runtime, nil
}

func (t *scriptedAdapterTransport) CreateTurn(_ context.Context, request modelturn.ModelRequest) (modelturn.Turn, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if request.Sequence != t.runtime.LastSequence+1 {
		return modelturn.Turn{}, modelturn.ErrSequenceMismatch
	}
	turnID := modelturn.TurnID(fmt.Sprintf("mt_%032x", request.Sequence))
	turn := modelturn.Turn{
		RuntimeID: request.RuntimeID, ID: turnID, Sequence: request.Sequence,
		RequestDigest: request.RequestDigest, CreatedAt: time.Now().UTC(),
	}
	var payload struct {
		Prompt []json.RawMessage `json:"prompt"`
		Tools  []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(request.Payload, &payload); err != nil {
		return modelturn.Turn{}, err
	}
	var responsePayload []byte
	if request.Sequence == 1 {
		var execToolID string
		for _, tool := range payload.Tools {
			if tool.Name == "exec_command" {
				execToolID = tool.ID
				break
			}
		}
		if execToolID == "" {
			return modelturn.Turn{}, errors.New("Codex request did not offer exec_command")
		}
		responsePayload, _ = json.Marshal(map[string]any{
			"finish_reason": "tool_calls",
			"tool_calls": []any{map[string]any{
				"arguments": map[string]any{"cmd": "printf " + toolMarker},
				"call_id":   "call_scripted_1",
				"tool_id":   execToolID,
			}},
		})
	} else {
		encoded := string(request.Payload)
		if !strings.Contains(encoded, toolMarker) || !strings.Contains(encoded, "tool-result") {
			return modelturn.Turn{}, errors.New("Codex request did not return the tool result")
		}
		t.toolResultSeen = true
		responsePayload, _ = json.Marshal(map[string]any{"finish_reason": "stop", "text": loopMarker})
	}
	t.runtime.LastSequence = request.Sequence
	t.responses[turnID] = modelturn.ModelResponse{
		RuntimeID: request.RuntimeID, TurnID: turnID, Sequence: request.Sequence,
		RequestDigest: request.RequestDigest, Payload: responsePayload,
	}
	return turn, nil
}

func (t *scriptedAdapterTransport) WaitResponse(_ context.Context, turnID modelturn.TurnID) (modelturn.ModelResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	response, exists := t.responses[turnID]
	if !exists {
		return modelturn.ModelResponse{}, modelturn.ErrTurnNotFound
	}
	return response, nil
}

func (*scriptedAdapterTransport) Cancel(context.Context, modelturn.TurnID) error {
	return nil
}

func verifiedCodexBinary(t *testing.T) string {
	t.Helper()
	bin := strings.TrimSpace(os.Getenv(codexBinaryEnv))
	if bin == "" {
		t.Skipf("set %s to run the official Codex host acceptance", codexBinaryEnv)
	}
	info, err := os.Lstat(bin)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		t.Fatalf("Codex artifact is not one executable regular file: mode=%s", info.Mode())
	}
	file, err := os.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		t.Fatal(err)
	}
	const expected = "cb0a15567e9a60a5820d54b0f6ae86d504dc3805c1eab21a47f70e3eb7b73a40"
	if actual := hex.EncodeToString(digest.Sum(nil)); actual != expected {
		t.Fatalf("Codex binary digest mismatch: got %s", actual)
	}
	return bin
}

func scrubModelCredentials(environment []string, codexHome string) []string {
	blocked := map[string]struct{}{
		"ANTHROPIC_API_KEY": {}, "AZURE_OPENAI_API_KEY": {}, "CODEX_API_KEY": {},
		"GOOGLE_API_KEY": {}, "OPENAI_API_KEY": {}, "OPENROUTER_API_KEY": {},
	}
	clean := make([]string, 0, len(environment)+2)
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if !found || strings.EqualFold(key, "CODEX_HOME") {
			continue
		}
		if _, denied := blocked[strings.ToUpper(key)]; denied {
			continue
		}
		clean = append(clean, entry)
	}
	return append(clean, "CODEX_HOME="+codexHome, "NO_COLOR=1")
}

func scriptedResponsesSSE(marker string) string {
	events := []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_mcp_devbox_spike"}},
		{"type": "response.output_item.done", "item": map[string]any{"type": "message", "role": "assistant", "id": "msg_mcp_devbox_spike", "content": []map[string]any{{"type": "output_text", "text": marker}}}},
		{"type": "response.completed", "response": map[string]any{"id": "resp_mcp_devbox_spike", "usage": map[string]any{"input_tokens": 0, "input_tokens_details": nil, "output_tokens": 0, "output_tokens_details": nil, "total_tokens": 0}}},
	}
	var output strings.Builder
	for _, event := range events {
		body, _ := json.Marshal(event)
		fmt.Fprintf(&output, "event: %s\ndata: %s\n\n", event["type"], body)
	}
	return output.String()
}
