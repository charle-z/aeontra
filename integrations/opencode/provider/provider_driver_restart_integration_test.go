//go:build !windows

package provider_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

type providerRestartResult struct {
	Status       string `json:"status"`
	CreateCalls  int    `json:"create_calls"`
	WaitCalls    int    `json:"wait_calls"`
	SameWaitPath bool   `json:"same_wait_path"`
}

func TestNodeProviderRetriesExactResponseAfterRealDriverRestart(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("Node.js is required for the provider integration test: %v", err)
	}
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	storeRoot := filepath.Join(root, "model-turns")
	controller, err := modelturn.OpenStore(modelturn.StoreConfig{Root: storeRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	runtimeState, err := controller.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	socketPath := filepath.Join(root, modelturn.DefaultDriverSocketName)
	firstStore, err := modelturn.OpenStore(modelturn.StoreConfig{Root: storeRoot})
	if err != nil {
		t.Fatal(err)
	}
	firstCtx, firstCancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { firstDone <- modelturn.ServeDriver(firstCtx, socketPath, firstStore, nil) }()
	waitForProviderSocket(t, socketPath)

	scriptPath := filepath.Join(t.TempDir(), "provider-restart.mjs")
	script := `
import { pathToFileURL } from "node:url";
const { createMCPDevboxModelBridge, __test } = await import(pathToFileURL(process.argv[2]).href);
const base = __test.createUnixRequester(process.argv[3]);
let created;
let createCalls = 0;
const waitPaths = [];
const requestImpl = async (request) => {
  if (request.method === "POST" && request.path === "/v1/turns") createCalls += 1;
  if (request.method === "GET" && request.path.endsWith("/response")) waitPaths.push(request.path);
  const value = await base(request);
  if (request.method === "POST" && request.path === "/v1/turns") created = value;
  return value;
};
const model = createMCPDevboxModelBridge({socketPath: process.argv[3], runtimeID: process.argv[4], requestImpl, ttlMs: 60000, timeoutMs: 10000}).languageModel("external-model");
const result = await model.doGenerate({prompt: [{role: "user", content: [{type: "text", text: "restart"}]}], tools: []});
if (result.content.length !== 1 || result.content[0].text !== "resumed") throw new Error("unexpected result");
const expectedWaitPath = "/v1/turns/" + created.turn_id + "/response";
process.stdout.write(JSON.stringify({
  status: "ok",
  create_calls: createCalls,
  wait_calls: waitPaths.length,
  same_wait_path: waitPaths.every((path) => path === expectedWaitPath),
}));
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	providerPath, err := filepath.Abs("index.js")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(node, scriptPath, providerPath, socketPath, runtimeState.RuntimeID)
	command.Dir = "."
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	turnCtx, turnCancel := context.WithTimeout(context.Background(), 10*time.Second)
	offer, err := controller.Next(turnCtx, runtimeState.RuntimeID)
	turnCancel()
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatal(err)
	}
	waitForRealDriverWait(t, socketPath)
	firstMetrics, err := readProviderDriverMetrics(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if firstMetrics.CreateCalls != 1 || firstMetrics.WaitCalls != 1 {
		t.Fatalf("first driver calls: create=%d wait=%d", firstMetrics.CreateCalls, firstMetrics.WaitCalls)
	}
	before, err := controller.Get(context.Background(), offer.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	if before.RuntimeID != runtimeState.RuntimeID || before.TurnID != offer.TurnID || before.Sequence != offer.Sequence || before.RequestDigest != offer.RequestDigest || before.RequestRef != offer.RequestRef || before.RequestRef == "" {
		t.Fatalf("initial turn identity mismatch")
	}

	firstCancel()
	select {
	case err := <-firstDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("first driver shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first driver shutdown timed out")
	}
	if err := firstStore.Close(); err != nil {
		t.Fatal(err)
	}

	secondStore, err := modelturn.OpenStore(modelturn.StoreConfig{Root: storeRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	secondCtx, secondCancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() { secondDone <- modelturn.ServeDriver(secondCtx, socketPath, secondStore, nil) }()
	waitForProviderSocket(t, socketPath)
	defer func() {
		secondCancel()
		select {
		case err := <-secondDone:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("second driver shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("second driver shutdown timed out")
		}
	}()

	after, err := controller.Get(context.Background(), offer.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	if before.RuntimeID != after.RuntimeID || before.TurnID != after.TurnID || before.Sequence != after.Sequence || before.RequestDigest != after.RequestDigest || before.RequestRef != after.RequestRef {
		t.Fatal("driver restart changed turn identity")
	}
	response := json.RawMessage(`{"finish_reason":"stop","text":"resumed","tool_calls":[],"usage":null}`)
	if _, err := controller.Respond(context.Background(), modelturn.ResponseSubmission{
		RuntimeID: runtimeState.RuntimeID, TurnID: offer.TurnID, ExpectedSequence: offer.Sequence,
		RequestDigest: offer.RequestDigest, Payload: response,
	}); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("provider restart failed: %v stderr=%q stdout=%q", err, stderr.String(), stdout.String())
	}
	var result providerRestartResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode provider result: %v", err)
	}
	// The number of failed socket attempts during the real process handoff is scheduler
	// dependent. The safety contract is one create, at least one retry, and every retry
	// bound to the exact same immutable response path.
	if result.Status != "ok" || result.CreateCalls != 1 || result.WaitCalls < 2 || !result.SameWaitPath {
		t.Fatalf("provider retry contract failed: status=%q create=%d wait=%d same_path=%t", result.Status, result.CreateCalls, result.WaitCalls, result.SameWaitPath)
	}
	secondMetrics, err := readProviderDriverMetrics(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if secondMetrics.CreateCalls != 0 || secondMetrics.WaitCalls != 1 {
		t.Fatalf("second driver calls: create=%d wait=%d", secondMetrics.CreateCalls, secondMetrics.WaitCalls)
	}
	stats, err := controller.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.TurnCount != 1 || stats.ConsumedCount != 1 || stats.AwaitingCount != 0 || stats.RespondedCount != 0 {
		t.Fatalf("stats=%+v", stats)
	}
	if _, err := controller.WaitResponse(context.Background(), offer.TurnID); !errors.Is(err, modelturn.ErrResponseReplay) {
		t.Fatalf("response was not consumed exactly once: %v", err)
	}
}

func waitForRealDriverWait(t *testing.T, socketPath string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		metrics, err := readProviderDriverMetrics(socketPath)
		if err == nil && metrics.WaitCalls > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("provider did not begin waiting for the response")
}

func readProviderDriverMetrics(socketPath string) (modelturn.DriverMetrics, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	response, err := client.Get("http://unix/v1/metrics")
	if err != nil {
		return modelturn.DriverMetrics{}, err
	}
	defer response.Body.Close()
	var metrics modelturn.DriverMetrics
	if err := json.NewDecoder(response.Body).Decode(&metrics); err != nil {
		return modelturn.DriverMetrics{}, err
	}
	return metrics, nil
}
