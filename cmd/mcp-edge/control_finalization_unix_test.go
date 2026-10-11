//go:build !windows

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type lostFinalizingReporter struct{ calls int }

func (r *lostFinalizingReporter) ReportOperationProgress(_ context.Context, _, _ string, p edge.OperationProgress) (edge.OperationControl, error) {
	r.calls++
	if p.Phase == "finalizing" {
		return edge.OperationControl{}, errors.New("response lost")
	}
	return edge.OperationControl{}, nil
}

type finalizingStoreReporter struct {
	store        *edge.Store
	device       string
	persistFinal bool
}

func (r *finalizingStoreReporter) ReportOperationProgress(_ context.Context, id, lease string, p edge.OperationProgress) (edge.OperationControl, error) {
	if p.Phase != "finalizing" || r.persistFinal {
		control, err := r.store.ReportOperationProgress(r.device, id, lease, p)
		if err != nil || p.Phase != "finalizing" {
			return control, err
		}
	}
	return edge.OperationControl{}, context.DeadlineExceeded
}

func TestFinalizationRecoveryUsesOriginalLeaseWithoutReexecution(t *testing.T) {
	for _, outcome := range []string{"request_lost", "response_lost", "cancelled", "expired"} {
		t.Run(outcome, func(t *testing.T) {
			now := time.Now().UTC()
			store, err := edge.Open(edge.Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			pairCode, err := store.CreatePairing(time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			pub, _, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			device, err := store.Pair(pairCode, "test-edge", pub)
			if err != nil {
				t.Fatal(err)
			}
			op, _, err := store.CreateOperation(device.ID, edge.OperationProjectExec, edge.OperationRequest{
				Alias: "repo", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "finalizing-recovery-test", Argv: []string{"true"}, TimeoutSeconds: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			lease, err := store.LeaseOperation(device.ID, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			r := &finalizingStoreReporter{store: store, device: device.ID, persistFinal: outcome != "request_lost"}
			executions := 0
			result, code, cancelled, progressErr := executeControlOperationLifecycle(context.Background(), r, lease, time.Hour, func(context.Context) (edge.OperationResult, string) {
				executions++
				return edge.OperationResult{
					WorkspaceID:  "ws_0123456789abcdef0123456789abcdef",
					ProjectAlias: "repo", ProjectOwner: "charle-z", ProjectRepository: "repo",
					ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
					ExecCompleted: true, ExecStdout: "once",
				}, ""
			})
			if !controlResultCanComplete(progressErr) || progressErr == nil || cancelled || executions != 1 {
				t.Fatalf("recovery=%v cancelled=%t executions=%d", progressErr, cancelled, executions)
			}
			if outcome == "cancelled" {
				if _, err := store.RequestOperationCancel(op.ID); err != nil {
					t.Fatal(err)
				}
			}
			if outcome == "expired" {
				now = now.Add(61 * time.Second)
			}
			completed, err := store.CompleteOperation(device.ID, op.ID, lease.LeaseID, result, code)
			if outcome == "cancelled" || outcome == "expired" {
				if err == nil {
					t.Fatal("accepted completion after cancellation/expiry")
				}
				now = now.Add(61 * time.Second)
			} else if err != nil || completed.State != edge.OperationSucceeded || completed.Result.ExecStdout != "once" {
				t.Fatalf("completion state=%s err=%v", completed.State, err)
			}
			if _, err := store.LeaseOperation(device.ID, time.Minute); err == nil {
				t.Fatal("operation was requeued for execution")
			}
			if executions != 1 {
				t.Fatal("executor replayed")
			}
		})
	}
}

func TestLostFinalizingResponsePreservesCompletedExecution(t *testing.T) {
	r := &lostFinalizingReporter{}
	executions := 0
	want := edge.OperationResult{ToolboxOutput: "execution receipt"}
	got, code, cancelled, err := executeControlOperationLifecycle(context.Background(), r, edge.OperationLease{}, time.Hour, func(context.Context) (edge.OperationResult, string) {
		executions++
		return want, "project_exec_failed"
	})
	if got.ToolboxOutput != want.ToolboxOutput || code != "project_exec_failed" || cancelled || executions != 1 || err == nil {
		t.Fatalf("result=%+v code=%q cancelled=%t executions=%d err=%v", got, code, cancelled, executions, err)
	}
}
