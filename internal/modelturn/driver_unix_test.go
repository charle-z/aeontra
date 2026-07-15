//go:build !windows

package modelturn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(value)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func TestDriverUnixSocketIsPrivateAndOwnedByEdgeProcess(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(StoreConfig{Root: filepath.Join(stateRoot, "model-turns")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	socketPath := filepath.Join(stateRoot, DefaultDriverSocketName)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := &lockedBuffer{}
	done := make(chan error, 1)
	go func() { done <- ServeDriver(ctx, socketPath, store, ready) }()
	waitForSocket(t, socketPath)

	info, err := os.Lstat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 || !ownedByEffectiveUID(info) {
		t.Fatalf("socket mode=%v owner_ok=%v", info.Mode(), ownedByEffectiveUID(info))
	}
	client := unixHTTPClient(socketPath)
	response, err := client.Get("http://unix/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), DriverProtocolVersion) {
		t.Fatalf("health status=%d body=%s", response.StatusCode, body)
	}
	if strings.Contains(ready.String(), "private-prompt") || !strings.Contains(ready.String(), `"socket_path"`) || strings.Contains(ready.String(), "token") {
		t.Fatalf("unsafe ready output=%q", ready.String())
	}

	if os.Geteuid() == 0 {
		command := exec.Command(os.Args[0], "-test.run=TestDriverUnixUnauthorizedHelper")
		command.Env = append(os.Environ(), "MCP_DEVBOX_UNAUTHORIZED_SOCKET_TEST=1", "MCP_DEVBOX_SOCKET_PATH="+socketPath)
		command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("unauthorized process reached or helper failed: %v output=%s", err, output)
		}
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("serve error=%v", err)
	}
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket was not removed: %v", err)
	}
}

func TestDriverUnixUnauthorizedHelper(t *testing.T) {
	if os.Getenv("MCP_DEVBOX_UNAUTHORIZED_SOCKET_TEST") != "1" {
		return
	}
	connection, err := net.DialTimeout("unix", os.Getenv("MCP_DEVBOX_SOCKET_PATH"), 250*time.Millisecond)
	if err == nil {
		_ = connection.Close()
		os.Exit(9)
	}
	os.Exit(0)
}

func TestDriverRestartPreservesAwaitingTurnAndExactReference(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	storeRoot := filepath.Join(stateRoot, "model-turns")
	control, err := OpenStore(StoreConfig{Root: storeRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	runtime, err := control.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(stateRoot, DefaultDriverSocketName)

	first, err := OpenStore(StoreConfig{Root: storeRoot})
	if err != nil {
		t.Fatal(err)
	}
	firstCancel, firstDone := startUnixDriver(t, socketPath, first)
	client := unixHTTPClient(socketPath)
	payload := json.RawMessage(`{"prompt":[{"content":"restart-marker","role":"user"}]}`)
	created := unixJSON(t, client, http.MethodPost, "/v1/turns", map[string]any{
		"runtime_id":     runtime.RuntimeID,
		"sequence":       1,
		"request_digest": digestBytes(payload),
		"payload":        payload,
		"ttl_ms":         60000,
	}, http.StatusCreated)
	var turn Turn
	decodeJSONMap(t, created, &turn)
	before, err := control.Get(context.Background(), turn.ID)
	if err != nil || before.Status != StatusAwaitingModel {
		t.Fatalf("before=%+v err=%v", before, err)
	}
	firstCancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first driver stop=%v", err)
	}
	_ = first.Close()

	second, err := OpenStore(StoreConfig{Root: storeRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	secondCancel, secondDone := startUnixDriver(t, socketPath, second)
	defer func() {
		secondCancel()
		<-secondDone
	}()
	after, err := second.Get(context.Background(), turn.ID)
	if err != nil || after.Status != StatusAwaitingModel || after.RequestRef != before.RequestRef || after.RequestDigest != before.RequestDigest || after.Sequence != before.Sequence {
		t.Fatalf("after=%+v before=%+v err=%v", after, before, err)
	}
	if _, err := control.Respond(context.Background(), ResponseSubmission{
		RuntimeID: runtime.RuntimeID, TurnID: turn.ID, ExpectedSequence: 1, RequestDigest: turn.RequestDigest,
		Payload: json.RawMessage(`{"finish_reason":"stop","text":"resumed","tool_calls":[]}`),
	}); err != nil {
		t.Fatal(err)
	}
	response := unixJSON(t, unixHTTPClient(socketPath), http.MethodGet, "/v1/turns/"+string(turn.ID)+"/response", nil, http.StatusOK)
	var modelResponse ModelResponse
	decodeJSONMap(t, response, &modelResponse)
	if modelResponse.TurnID != turn.ID || !strings.Contains(string(modelResponse.Payload), `"resumed"`) {
		t.Fatalf("response=%+v", modelResponse)
	}
}

func TestDriverRefusesUnsafeOrActiveSocket(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	unsafe := filepath.Join(root, "unsafe.sock")
	if err := os.WriteFile(unsafe, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDriverSocketPath(unsafe); err == nil {
		t.Fatal("regular file accepted as socket")
	}
	listener, err := net.Listen("unix", filepath.Join(root, "active.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := prepareDriverSocketPath(listener.Addr().String()); err == nil {
		t.Fatal("active socket accepted")
	}
}

func startUnixDriver(t *testing.T, socketPath string, store *Store) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeDriver(ctx, socketPath, store, io.Discard) }()
	waitForSocket(t, socketPath)
	return cancel, done
}

func waitForSocket(t *testing.T, socketPath string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Lstat(socketPath); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket did not appear: %s", socketPath)
}

func unixHTTPClient(socketPath string) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

func unixJSON(t *testing.T, client *http.Client, method, path string, value any, wantStatus int) map[string]any {
	t.Helper()
	var body io.Reader
	if value != nil {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, "http://unix"+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var decoded map[string]any
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("status=%d want=%d body=%v", response.StatusCode, wantStatus, decoded)
	}
	return decoded
}

func TestDriverReadyOutputContainsNoSecrets(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(StoreConfig{Root: filepath.Join(stateRoot, "model-turns")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ready := &lockedBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	socketPath := filepath.Join(stateRoot, DefaultDriverSocketName)
	go func() { done <- ServeDriver(ctx, socketPath, store, ready) }()
	waitForSocket(t, socketPath)
	cancel()
	<-done
	output := ready.String()
	if strings.Contains(output, "API_KEY") || strings.Contains(output, "Bearer") || strings.Contains(output, "private-prompt") {
		t.Fatalf("ready output leaked secret-like content: %q", output)
	}
	if !strings.Contains(output, strconv.Itoa(os.Geteuid())) {
		t.Fatalf("ready output omitted owner uid: %q", output)
	}
}
