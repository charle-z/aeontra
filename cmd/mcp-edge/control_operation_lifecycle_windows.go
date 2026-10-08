//go:build windows

package main

import (
	"context"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type windowsControlOperationExecution struct {
	result edge.OperationResult
	code   string
}

type windowsOperationProgressReporter interface {
	ReportOperationProgress(context.Context, string, string, edge.OperationProgress) (edge.OperationControl, error)
}

// executeWindowsControlOperationWithProgress keeps the operation lease owned
// until the executor exits. In particular, cancellation only acknowledges at
// the end; it never releases a lease while a Windows Job Object may still
// have descendants alive.
func executeWindowsControlOperationWithProgress(ctx context.Context, stateRoot string, transport windowsOperationProgressReporter, processes *edgeclient.ProjectProcessManager, workspaceCount int, lease edge.OperationLease) (edge.OperationResult, string, bool, error) {
	result, code, cancelRequested, err, _, _ := executeWindowsControlOperationWithProgressAndGate(ctx, stateRoot, transport, processes, workspaceCount, nil, lease)
	return result, code, cancelRequested, err
}

func executeWindowsControlOperationWithProgressAndGate(ctx context.Context, stateRoot string, transport windowsOperationProgressReporter, processes *edgeclient.ProjectProcessManager, workspaceCount int, controlGate *controlOperationGate, lease edge.OperationLease) (edge.OperationResult, string, bool, error, bool, bool) {
	exclusive := windowsBundleOperation(lease.Operation.Kind)
	gateHeld := false
	result, code, cancelRequested, err := executeWindowsControlOperationLifecycle(ctx, transport, lease, func(executionCtx context.Context) (edge.OperationResult, string) {
		if controlGate != nil {
			if !controlGate.acquire(executionCtx, exclusive) {
				return edge.OperationResult{}, "cancelled"
			}
			gateHeld = true
		}
		return executeWindowsControlOperation(executionCtx, stateRoot, processes, workspaceCount, lease.Operation)
	})
	return result, code, cancelRequested, err, gateHeld, exclusive
}

func windowsBundleOperation(kind edge.OperationKind) bool {
	return kind == edge.OperationBundleUpdate || kind == edge.OperationBundleRollback || kind == edge.OperationEdgeRepair
}

func executeWindowsControlOperationLifecycle(ctx context.Context, transport windowsOperationProgressReporter, lease edge.OperationLease, execute func(context.Context) (edge.OperationResult, string)) (edge.OperationResult, string, bool, error) {
	progressCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	control, err := transport.ReportOperationProgress(progressCtx, lease.Operation.ID, lease.LeaseID, edge.OperationProgress{Revision: 1, Phase: controlOperationPhase(lease.Operation.Kind)})
	cancel()
	if err != nil {
		return edge.OperationResult{}, "", false, errors.New("Windows control operation progress failed")
	}
	if control.CancelRequested {
		return edge.OperationResult{}, "", true, nil
	}
	executionCtx, cancelExecution := context.WithCancel(ctx)
	defer cancelExecution()
	completed := make(chan windowsControlOperationExecution, 1)
	go func() {
		result, code := execute(executionCtx)
		completed <- windowsControlOperationExecution{result: result, code: code}
	}()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	revision := uint64(1)
	cancelRequested := false
	for {
		select {
		case execution := <-completed:
			revision++
			progressCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			control, progressErr := transport.ReportOperationProgress(progressCtx, lease.Operation.ID, lease.LeaseID, edge.OperationProgress{Revision: revision, Phase: "finalizing"})
			cancel()
			if progressErr != nil {
				return edge.OperationResult{}, "", cancelRequested, progressErr
			}
			if control.CancelRequested {
				cancelRequested = true
			}
			return execution.result, execution.code, cancelRequested, nil
		case <-ticker.C:
			revision++
			phase := controlOperationPhase(lease.Operation.Kind)
			if cancelRequested {
				phase = "stopping"
			}
			progressCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			control, progressErr := transport.ReportOperationProgress(progressCtx, lease.Operation.ID, lease.LeaseID, edge.OperationProgress{Revision: revision, Phase: phase})
			cancel()
			if progressErr != nil {
				cancelExecution()
				// Keep the gate held until the executor exits. A failed heartbeat
				// does not prove that a Windows Job Object has stopped.
				<-completed
				return edge.OperationResult{}, "", cancelRequested, progressErr
			}
			if control.CancelRequested && !cancelRequested {
				cancelRequested = true
				cancelExecution()
			}
		case <-ctx.Done():
			cancelExecution()
			execution := <-completed
			return execution.result, execution.code, cancelRequested, ctx.Err()
		}
	}
}

func controlOperationPhase(kind edge.OperationKind) string {
	switch kind {
	case edge.OperationBundleUpdate:
		return "updating"
	case edge.OperationBundleRollback:
		return "rolling_back"
	case edge.OperationEdgeRepair:
		return "repairing"
	default:
		return "running"
	}
}
