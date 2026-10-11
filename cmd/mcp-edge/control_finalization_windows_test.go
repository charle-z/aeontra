//go:build windows

package main

import (
	"context"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type windowsLostFinalizingReporter struct{ initialFailure bool }

func (r windowsLostFinalizingReporter) ReportOperationProgress(_ context.Context, _, _ string, progress edge.OperationProgress) (edge.OperationControl, error) {
	if r.initialFailure || progress.Phase == "finalizing" {
		return edge.OperationControl{}, context.DeadlineExceeded
	}
	return edge.OperationControl{}, nil
}

func TestWindowsFinalizationPreservesResultWithoutReexecution(t *testing.T) {
	for _, initialFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "finalizing_lost", true: "initial_progress_lost"}[initialFailure], func(t *testing.T) {
			executions := 0
			result, code, cancelled, err := executeWindowsControlOperationLifecycle(context.Background(), windowsLostFinalizingReporter{initialFailure}, edge.OperationLease{}, func(context.Context) (edge.OperationResult, string) {
				executions++
				return edge.OperationResult{ExecCompleted: true, ExecStdout: "once"}, "project_exec_failed"
			})
			if err == nil || cancelled {
				t.Fatalf("err=%v cancelled=%t", err, cancelled)
			}
			if initialFailure {
				if executions != 0 || controlResultCanComplete(err) || result.ExecCompleted {
					t.Fatal("initial progress failure permitted execution or completion")
				}
				return
			}
			if executions != 1 || !controlResultCanComplete(err) || !result.ExecCompleted || result.ExecStdout != "once" || code != "project_exec_failed" {
				t.Fatalf("result=%+v code=%q executions=%d err=%v", result, code, executions, err)
			}
		})
	}
}
