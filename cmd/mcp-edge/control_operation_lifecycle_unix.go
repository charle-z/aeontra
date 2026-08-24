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
	return executeControlOperationLifecycle(ctx, transport, lease, 15*time.Second, func(executionCtx context.Context) (edge.OperationResult, string) {
		return executeControlOperation(executionCtx, stateRoot, processes, browsers, lease.Operation)
	})
}

func executeControlOperationLifecycle(ctx context.Context, transport controlOperationProgressReporter, lease edge.OperationLease, progressInterval time.Duration, execute controlOperationExecutor) (edge.OperationResult, string, bool, error) {
	revision := uint64(1)
	control, err := reportControlOperationProgress(ctx, transport, lease, revision, "running")
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
			phase := "running"
			if cancelRequested {
				phase = "stopping"
			}
			control, err := reportControlOperationProgress(ctx, transport, lease, revision, phase)
			if err != nil {
				cancelExecution()
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
