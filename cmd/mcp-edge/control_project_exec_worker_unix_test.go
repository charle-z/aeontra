//go:build !windows

package main

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

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestProjectExecWorkerPreservesResultWithoutWorkspaceReregistration(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("Git is required for the registered checkout fixture")
	}
	for _, finalizingFault := range []string{"none", "request_lost", "response_lost"} {
		t.Run(finalizingFault, func(t *testing.T) {
			fixtureHome := t.TempDir()
			t.Setenv("HOME", fixtureHome)
			stateRoot := t.TempDir()
			roots, err := edgeclient.DefaultWorkspaceRoots()
			if err != nil {
				t.Fatal(err)
			}
			workspacePath := filepath.Join(roots.Dev, "worker-fixture")
			for _, path := range []string{workspacePath, roots.HTBLinux} {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			git := func(args ...string) {
				t.Helper()
				command := exec.CommandContext(t.Context(), gitPath, append([]string{"-c", "core.hooksPath=/dev/null", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test"}, args...)...)
				command.Dir = workspacePath
				command.Env = []string{"HOME=" + fixtureHome, "PATH=/usr/local/bin:/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0"}
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("fixture Git: %v: %s", err, output)
				}
			}
			git("init", "--quiet")
			git("remote", "add", "origin", "https://github.com/charle-z/worker-fixture.git")
			git("commit", "--quiet", "--allow-empty", "-m", "test: initialize worker fixture")
			if _, err := edgeclient.ConfigureGitHubCredential(stateRoot, "charle-z", strings.NewReader("fixture-only-authority-not-a-real-token")); err != nil {
				t.Fatal(err)
			}
			workspaces, err := edgeclient.OpenWorkspaceRegistryWithRoots(stateRoot, roots)
			if err != nil {
				t.Fatal(err)
			}
			workspace, _, err := workspaces.AddProfile(workspacePath, edgeclient.WorkspaceProfileLinuxWorkcell)
			if err != nil {
				_ = workspaces.Close()
				t.Fatal(err)
			}
			projects, err := edgeclient.OpenProjectRegistry(edgeclient.ProjectRegistryConfig{StateRoot: stateRoot, AllowedOwner: "charle-z", Workspaces: workspaces})
			if err != nil {
				_ = workspaces.Close()
				t.Fatal(err)
			}
			_, _, registerErr := projects.Register(edgeclient.ProjectRegistration{
				Alias: "project", Owner: "charle-z", Repository: "worker-fixture", PreferredTarget: "parrot", TargetAlias: "parrot",
				WorkspaceID: workspace.ID, AllowedProfiles: []edgeclient.WorkspaceProfile{edgeclient.WorkspaceProfileLinuxWorkcell},
			})
			_ = projects.Close()
			_ = workspaces.Close()
			if registerErr != nil {
				t.Fatal(registerErr)
			}
			// This fixture executes only the supplied test argv in its temporary
			// checkout. It tests worker finalization, not Bubblewrap isolation.
			fixtureBin := t.TempDir()
			bubblewrap := "#!/bin/sh\nwhile [ \"$#\" -gt 0 ] && [ \"$1\" != \"--\" ]; do shift; done\n[ \"$#\" -gt 0 ] || exit 99\nshift\nexec \"$@\"\n"
			if err := os.WriteFile(filepath.Join(fixtureBin, "bwrap"), []byte(bubblewrap), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", fixtureBin+string(os.PathListSeparator)+os.Getenv("PATH"))
			store, err := edge.Open(edge.Config{Root: filepath.Join(t.TempDir(), "server-edge")})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			pairCode, err := store.CreatePairing(time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			workerCtx, cancelWorker := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancelWorker()
			var registrations, completions, finalizingReports atomic.Int32
			var originalLease, completedLease atomic.Value
			base := edge.NewHTTPHandler(store)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/edge/v1/workspaces/register" {
					registrations.Add(1)
					http.Error(w, "fixture registration unavailable", http.StatusServiceUnavailable)
					return
				}
				if r.URL.Path == "/edge/v1/operations/lease" && completions.Load() > 0 {
					cancelWorker()
					w.WriteHeader(http.StatusNoContent)
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("fixture request body: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				isFinalizing := false
				if strings.HasSuffix(r.URL.Path, "/progress") {
					var request struct {
						Progress edge.OperationProgress `json:"progress"`
					}
					if err := json.Unmarshal(body, &request); err != nil {
						t.Errorf("fixture progress: %v", err)
					}
					isFinalizing = request.Progress.Phase == "finalizing"
					if isFinalizing {
						finalizingReports.Add(1)
						if finalizingFault == "request_lost" {
							http.Error(w, "fixture progress unavailable", http.StatusServiceUnavailable)
							return
						}
					}
				}
				if strings.HasSuffix(r.URL.Path, "/complete") {
					completions.Add(1)
					var request struct {
						LeaseID string `json:"lease_id"`
					}
					if err := json.Unmarshal(body, &request); err != nil {
						t.Errorf("fixture completion: %v", err)
					}
					completedLease.Store(request.LeaseID)
				}
				response := httptest.NewRecorder()
				base.ServeHTTP(response, r)
				if r.URL.Path == "/edge/v1/operations/lease" && response.Code == http.StatusOK {
					var lease edge.OperationLease
					if err := json.Unmarshal(response.Body.Bytes(), &lease); err != nil {
						t.Errorf("fixture lease: %v", err)
					}
					originalLease.Store(lease.LeaseID)
				}
				for key, values := range response.Header() {
					w.Header()[key] = values
				}
				w.WriteHeader(response.Code)
				if isFinalizing && finalizingFault == "response_lost" {
					_, _ = io.WriteString(w, "{")
					return
				}
				_, _ = w.Write(response.Body.Bytes())
			}))
			defer server.Close()
			identity, err := edgeclient.Pair(t.Context(), edgeclient.PairOptions{ServerURL: server.URL, Code: pairCode, Name: "parrot", StateRoot: stateRoot, HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.RegisterWorkspaces(identity.DeviceID, []edge.WorkspaceRegistration{{WorkspaceID: workspace.ID, Profile: "linux-workcell", Mode: "dev"}}); err != nil {
				t.Fatal(err)
			}
			transport, err := edgeclient.NewTransport(stateRoot, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			operation, _, err := store.CreateOperation(identity.DeviceID, edge.OperationProjectExec, edge.OperationRequest{
				Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "worker-result-once",
				Argv: []string{"/bin/sh", "-c", "printf 'once\\n' >> execution-count\nprintf 'result-once\\n'\n"}, TimeoutSeconds: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			runControlOperationWorker(workerCtx, stateRoot, transport, nil, nil, nil, &controlOperationGate{}, &logs)
			completed, err := store.OperationStatus(operation.ID)
			if err != nil || completed.State != edge.OperationSucceeded || !completed.Result.ExecCompleted || completed.Result.ExecStdout != "result-once\n" {
				t.Fatalf("worker state=%s code=%s completed=%t stdout=%q err=%v logs=%s", completed.State, completed.SafeCode, completed.Result.ExecCompleted, completed.Result.ExecStdout, err, logs.String())
			}
			if registrations.Load() != 0 || completions.Load() != 1 || finalizingReports.Load() != 1 {
				t.Fatalf("registrations=%d completions=%d finalizing=%d", registrations.Load(), completions.Load(), finalizingReports.Load())
			}
			if originalLease.Load() == nil || originalLease.Load() != completedLease.Load() {
				t.Fatal("completion did not use the original lease")
			}
			executions, err := os.ReadFile(filepath.Join(workspacePath, "execution-count"))
			if err != nil || string(executions) != "once\n" {
				t.Fatalf("executor count=%q err=%v", executions, err)
			}
			if _, err := store.LeaseOperation(identity.DeviceID, time.Minute); err == nil {
				t.Fatal("completed operation was requeued")
			}
		})
	}
}
