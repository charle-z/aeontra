//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestWindowsControlWorkerOnceReturnsTerminalAcknowledgementError(t *testing.T) {
	originalDiagnostic := collectWindowsEdgeDiagnostic
	t.Cleanup(func() { collectWindowsEdgeDiagnostic = originalDiagnostic })
	for _, test := range []struct {
		name         string
		cancel       bool
		reject       bool
		finalizeLost bool
	}{
		{name: "completion accepted after lost finalizing", finalizeLost: true},
		{name: "completion rejected after lost finalizing", reject: true, finalizeLost: true},
		{name: "cancellation accepted", cancel: true},
		{name: "cancellation rejected", cancel: true, reject: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var executions, finalizations, completions, cancellations atomic.Int32
			collectWindowsEdgeDiagnostic = func(int) (edge.OperationResult, string) {
				executions.Add(1)
				return edge.OperationResult{
					Release: "v1.2.25", Commit: strings.Repeat("a", 40), ManifestStatus: "valid",
					ComponentsCompatible: true, ProviderValid: true, DriverValid: true,
				}, ""
			}
			store, err := edge.Open(edge.Config{Root: filepath.Join(t.TempDir(), "server-edge")})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			pairCode, err := store.CreatePairing(time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			base := edge.NewHTTPHandler(store)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/progress") {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Errorf("fixture progress body: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(body))
					var request struct {
						Progress edge.OperationProgress `json:"progress"`
					}
					if err := json.Unmarshal(body, &request); err != nil {
						t.Errorf("fixture progress: %v", err)
					}
					if request.Progress.Phase == "finalizing" {
						finalizations.Add(1)
						if test.finalizeLost {
							http.Error(w, "fixture progress unavailable", http.StatusServiceUnavailable)
							return
						}
						if test.cancel {
							operationID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/edge/v1/operations/"), "/progress")
							if _, err := store.RequestOperationCancel(operationID); err != nil {
								t.Errorf("fixture cancellation: %v", err)
							}
						}
					}
				}
				if strings.HasSuffix(r.URL.Path, "/complete") {
					completions.Add(1)
					if test.reject {
						http.Error(w, "fixture completion rejected", http.StatusConflict)
						return
					}
				}
				if strings.HasSuffix(r.URL.Path, "/cancel") {
					cancellations.Add(1)
					if test.reject {
						http.Error(w, "fixture cancellation rejected", http.StatusConflict)
						return
					}
				}
				base.ServeHTTP(w, r)
			}))
			defer server.Close()
			stateRoot := filepath.Join(t.TempDir(), "client-edge")
			identity, err := edgeclient.Pair(t.Context(), edgeclient.PairOptions{ServerURL: server.URL, Code: pairCode, Name: "windows-edge", StateRoot: stateRoot, HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			transport, err := edgeclient.NewTransport(stateRoot, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			registry, err := edgeclient.OpenWorkspaceRegistry(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			defer registry.Close()
			operation, _, err := store.CreateOperation(identity.DeviceID, edge.OperationBundleStatus, edge.OperationRequest{})
			if err != nil {
				t.Fatal(err)
			}
			gate := &controlOperationGate{}
			config := windowsAgentConfig{stateRoot: stateRoot, leaseTTL: time.Minute, once: true, stderr: io.Discard}
			workerCtx, cancelWorker := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancelWorker()
			err = runWindowsControlOperationWorker(workerCtx, config, transport, registry, nil, gate)
			if (err != nil) != test.reject {
				t.Fatalf("one-shot error=%v; rejection=%t", err, test.reject)
			}
			if test.reject && edgeclient.TransportErrorClass(err) != "conflict" {
				t.Fatalf("one-shot returned an unrelated error: %v", err)
			}
			if !gate.mu.TryLock() {
				t.Fatal("worker returned before releasing its gate")
			}
			gate.mu.Unlock()
			if executions.Load() != 1 || finalizations.Load() != 1 || completions.Load()+cancellations.Load() != 1 || (cancellations.Load() == 1) != test.cancel {
				t.Fatalf("executions=%d finalizations=%d completions=%d cancellations=%d", executions.Load(), finalizations.Load(), completions.Load(), cancellations.Load())
			}
			accepted, err := store.OperationStatus(operation.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantState := edge.OperationSucceeded
			if test.cancel {
				wantState = edge.OperationCancelled
			}
			if test.reject {
				wantState = edge.OperationLeased
			}
			if accepted.State != wantState {
				t.Fatalf("operation state=%s; want %s", accepted.State, wantState)
			}
		})
	}
}
