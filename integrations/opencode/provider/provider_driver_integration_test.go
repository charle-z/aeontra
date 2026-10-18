//go:build !windows

package provider_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestNodeProviderLargeRequestAgainstRealUnixDriver(t *testing.T) {
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
	driverStore, err := modelturn.OpenStore(modelturn.StoreConfig{Root: storeRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer driverStore.Close()
	runtimeState, err := controller.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	socketPath := filepath.Join(root, modelturn.DefaultDriverSocketName)
	driverCtx, driverCancel := context.WithCancel(context.Background())
	driverDone := make(chan error, 1)
	go func() { driverDone <- modelturn.ServeDriver(driverCtx, socketPath, driverStore, nil) }()
	waitForProviderSocket(t, socketPath)
	defer func() {
		driverCancel()
		select {
		case err := <-driverDone:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("driver shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("driver shutdown timed out")
		}
	}()

	scriptPath := filepath.Join(t.TempDir(), "provider-integration.mjs")
	script := `
import { pathToFileURL } from "node:url";
const [{ createMCPDevboxModelBridge }] = await Promise.all([import(pathToFileURL(process.argv[2]).href)]);
const socketPath = process.argv[3];
const runtimeID = process.argv[4];
const model = createMCPDevboxModelBridge({socketPath, runtimeID, ttlMs: 60000, timeoutMs: 10000}).languageModel("external-model");
const result = await model.doGenerate({
  prompt: [{role: "user", content: [{type: "text", text: "x".repeat((64 << 10) + 4096)}]}],
  tools: [],
});
if (result.content.length !== 1 || result.content[0].type !== "text" || result.content[0].text !== "accepted") {
  throw new Error("unexpected provider result shape");
}
process.stdout.write(JSON.stringify({status: "ok"}));
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
		t.Fatalf("next turn: %v stderr=%q", err, stderr.String())
	}
	if offer.Sequence != 1 || offer.RequestRef == "" || int64(len(offer.RequestPayload)) <= modelturn.MaxInlineRequestBytes {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("large request did not use request_ref: sequence=%d ref=%q bytes=%d", offer.Sequence, offer.RequestRef, len(offer.RequestPayload))
	}
	response := json.RawMessage(`{"finish_reason":"stop","text":"accepted","tool_calls":[],"usage":null}`)
	if _, err := controller.Respond(context.Background(), modelturn.ResponseSubmission{
		RuntimeID:        runtimeState.RuntimeID,
		TurnID:           offer.TurnID,
		ExpectedSequence: offer.Sequence,
		RequestDigest:    offer.RequestDigest,
		Payload:          response,
	}); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatal(err)
	}

	if err := command.Wait(); err != nil {
		t.Fatalf("real provider failed: %v stderr=%q stdout=%q", err, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `"status":"ok"`) {
		t.Fatalf("provider output=%q", stdout.String())
	}
	stats, err := controller.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.TurnCount != 1 || stats.ConsumedCount != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func waitForProviderSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("provider driver socket did not appear")
}
