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

func executeControlOperationWithProgress(ctx context.Context, stateRoot string, transport *edgeclient.Transport, processes *edgeclient.ProjectProcessManager, lease edge.OperationLease) (edge.OperationResult, string, bool, error) {
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
		result, code := executeControlOperation(executionCtx, stateRoot, processes, lease.Operation)
		completed <- controlOperationExecution{result: result, code: code}
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	cancelRequested := false
	var cancellationDeadline <-chan time.Time
	var cancellationTimer *time.Timer
	defer func() {
		if cancellationTimer != nil {
			cancellationTimer.Stop()
		}
	}()

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
				cancellationTimer = time.NewTimer(20 * time.Second)
				cancellationDeadline = cancellationTimer.C
			}
		case <-cancellationDeadline:
			return edge.OperationResult{}, "", true, errors.New("control operation cancellation timed out")
		case <-ctx.Done():
			cancelExecution()
			return edge.OperationResult{}, "", cancelRequested, ctx.Err()
		}
	}
}

func reportControlOperationProgress(ctx context.Context, transport *edgeclient.Transport, lease edge.OperationLease, revision uint64, phase string) (edge.OperationControl, error) {
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
