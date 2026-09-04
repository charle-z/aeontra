//go:build !windows

package main

import (
	"context"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type controlOperationExecution struct {
	result edge.OperationResult
	code   string
}

type controlOperationProgressReporter interface {
	ReportOperationProgress(context.Context, string, string, edge.OperationProgress) (edge.OperationControl, error)
}

type controlOperationExecutor func(context.Context) (edge.OperationResult, string)

func executeControlOperationWithProgress(ctx context.Context, stateRoot string, transport *edgeclient.Transport, processes *edgeclient.ProjectProcessManager, browsers *edgeclient.ProjectBrowserManager, lease edge.OperationLease) (edge.OperationResult, string, bool, error) {
	result, code, cancelRequested, err, _, _ := executeControlOperationWithProgressAndGate(ctx, stateRoot, transport, processes, browsers, nil, lease)
	return result, code, cancelRequested, err
}

func executeControlOperationWithProgressAndGate(ctx context.Context, stateRoot string, transport *edgeclient.Transport, processes *edgeclient.ProjectProcessManager, browsers *edgeclient.ProjectBrowserManager, controlGate *controlOperationGate, lease edge.OperationLease) (edge.OperationResult, string, bool, error, bool, bool) {
	exclusive := isBundleOperation(lease.Operation.Kind)
	gateHeld := false
	result, code, cancelRequested, err := executeControlOperationLifecycle(ctx, transport, lease, 15*time.Second, func(executionCtx context.Context) (edge.OperationResult, string) {
		if controlGate != nil {
			if !controlGate.acquire(executionCtx, exclusive) {
				return edge.OperationResult{}, "cancelled"
			}
			gateHeld = true
		}
		// The worker releases the gate after the durable completion/cancel
		// acknowledgement. Holding it only around this closure permits a second
		// bundle operation to race the first operation's receipt cleanup.
		return executeControlOperation(executionCtx, stateRoot, processes, browsers, lease.Operation)
	})
	return result, code, cancelRequested, err, gateHeld, exclusive
}

func executeControlOperationLifecycle(ctx context.Context, transport controlOperationProgressReporter, lease edge.OperationLease, progressInterval time.Duration, execute controlOperationExecutor) (edge.OperationResult, string, bool, error) {
	revision := uint64(1)
	control, err := reportControlOperationProgress(ctx, transport, lease, revision, controlOperationPhase(lease.Operation.Kind))
	if err != nil {
		return edge.OperationResult{}, "", false, err
	}
	if control.CancelRequested {
		return edge.OperationResult{}, "", true, nil
	}

	executionCtx, cancelExecution := context.WithCancel(ctx)
	defer cancelExecution()
	completed := make(chan controlOperationExecution, 1)
	go func() {
		result, code := execute(executionCtx)
		completed <- controlOperationExecution{result: result, code: code}
	}()

	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()
	cancelRequested := false

	for {
		select {
		case execution := <-completed:
			revision++
			control, err := reportControlOperationProgress(ctx, transport, lease, revision, "finalizing")
			if err != nil {
				return edge.OperationResult{}, "", cancelRequested, err
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
			control, err := reportControlOperationProgress(ctx, transport, lease, revision, phase)
			if err != nil {
				cancelExecution()
				// Do not let the worker release the operation gate while the
				// executor can still mutate shared state. A progress transport
				// failure is not evidence that execution stopped.
				<-completed
				return edge.OperationResult{}, "", cancelRequested, err
			}
			if control.CancelRequested && !cancelRequested {
				cancelRequested = true
				cancelExecution()
			}
		case <-ctx.Done():
			cancelExecution()
			// Preserve structured ownership: the leased effect must not outlive the
			// control operation goroutine. Service shutdown may wait for a
			// cooperative executor, but it cannot release the lease while the
			// executor is still mutating state.
			execution := <-completed
			return execution.result, execution.code, cancelRequested, ctx.Err()
		}
	}
}

// controlOperationPhase gives long-running administrative operations a stable,
// bounded phase visible to callers. Ordinary development work retains the
// historical running value; finalizing is emitted by the lifecycle itself.
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

func reportControlOperationProgress(ctx context.Context, transport controlOperationProgressReporter, lease edge.OperationLease, revision uint64, phase string) (edge.OperationControl, error) {
	progressCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	control, err := transport.ReportOperationProgress(progressCtx, lease.Operation.ID, lease.LeaseID, edge.OperationProgress{Revision: revision, Phase: phase})
	if err != nil {
		return edge.OperationControl{}, errors.New("control operation progress failed")
	}
	return control, nil
}

func acknowledgeControlOperationCancellation(ctx context.Context, stateRoot string, transport *edgeclient.Transport, lease edge.OperationLease) error {
	cancelCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := transport.CancelOperation(cancelCtx, lease.Operation.ID, lease.LeaseID)
	if err != nil {
		return err
	}
	if isBundleOperation(lease.Operation.Kind) {
		clearBundleReceipt(stateRoot, lease.Operation.ID)
	}
	return nil
}
