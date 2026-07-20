//go:build !windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/autopilot"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func runControlOperationLoop(ctx context.Context, stateRoot string, transport *edgeclient.Transport, stderr io.Writer) {
	for {
		if ctx.Err() != nil {
			return
		}
		lease, err := transport.LeaseOperation(ctx, time.Minute)
		if err != nil {
			fmt.Fprintln(stderr, "mcp-edge: control operation polling failed safely")
			if !waitControlOperation(ctx, 5*time.Second) {
				return
			}
			continue
		}
		if lease == nil {
			if !waitControlOperation(ctx, 2*time.Second) {
				return
			}
			continue
		}
		result, code := executeControlOperation(ctx, stateRoot, lease.Operation)
		if code == "" {
			registry, openErr := edgeclient.OpenWorkspaceRegistry(stateRoot)
			if openErr == nil {
				workspaces, listErr := registry.List()
				_ = registry.Close()
				if listErr != nil || transport.RegisterWorkspaces(ctx, workspaces) != nil {
					code = "workspace_registration_failed"
					result = edge.OperationResult{}
				}
			} else {
				code = "workspace_registration_failed"
				result = edge.OperationResult{}
			}
		}
		completionCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_, err = transport.CompleteOperation(completionCtx, lease.Operation.ID, lease.LeaseID, result, code)
		if err == nil && result.JobID != "" {
			err = transport.ReportAutopilot(completionCtx, result)
		}
		cancel()
		if err != nil {
			fmt.Fprintln(stderr, "mcp-edge: control operation completion failed safely")
		}
	}
}

func executeControlOperation(ctx context.Context, stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
	var output strings.Builder
	switch operation.Kind {
	case edge.OperationLabPrepare:
		request := operation.Request
		err := labInit([]string{"--state", stateRoot, "--platform", request.Platform, "--machine", request.Machine, "--target", request.Target, "--difficulty", request.Difficulty, "--os", request.OperatingSystem}, &output, io.Discard)
		if err != nil {
			return edge.OperationResult{}, safeControlFailure(err)
		}
		return resolvePreparedWorkspace(stateRoot, request.Machine)
	case edge.OperationLabRetarget:
		err := labRetarget([]string{"--state", stateRoot, "--workspace-id", operation.Request.WorkspaceID, "--target", operation.Request.Target}, &output, io.Discard)
		if err != nil {
			return edge.OperationResult{}, safeControlFailure(err)
		}
		return resolveOperationWorkspace(stateRoot, operation.Request.WorkspaceID)
	case edge.OperationAutopilotStart, edge.OperationAutopilotPause, edge.OperationAutopilotResume, edge.OperationAutopilotCancel:
		return executeAutopilotControl(stateRoot, operation)
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
}

func executeAutopilotControl(stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
	registry, err := edgeclient.OpenWorkspaceRegistry(stateRoot)
	if err != nil {
		return edge.OperationResult{}, "workspace_unavailable"
	}
	workspace, err := registry.Get(operation.Request.WorkspaceID)
	_ = registry.Close()
	if err != nil || workspace.Mode != edgeclient.WorkspaceModeHTBLinux {
		return edge.OperationResult{}, "workspace_unavailable"
	}
	store := autopilot.Store{Workspace: workspace.Path}
	var job autopilot.State
	switch operation.Kind {
	case edge.OperationAutopilotStart:
		job, _, err = store.Start(workspace.ID, operation.Request.RunUntil)
	case edge.OperationAutopilotPause:
		job, err = store.Pause()
	case edge.OperationAutopilotResume:
		job, err = store.Resume()
	case edge.OperationAutopilotCancel:
		job, err = store.Cancel()
	}
	if err != nil {
		return edge.OperationResult{}, "autopilot_control_failed"
	}
	return edge.OperationResult{WorkspaceID: workspace.ID, JobID: job.JobID, JobState: string(job.State), ProgressRevision: job.ProgressRevision, CycleCount: job.CycleCount, JobSafeCode: job.SafeCode}, ""
}

func resolvePreparedWorkspace(stateRoot, machine string) (edge.OperationResult, string) {
	registry, err := edgeclient.OpenWorkspaceRegistry(stateRoot)
	if err != nil {
		return edge.OperationResult{}, "workspace_unavailable"
	}
	defer registry.Close()
	workspaces, err := registry.List()
	if err != nil {
		return edge.OperationResult{}, "workspace_unavailable"
	}
	for _, workspace := range workspaces {
		if workspace.Mode == edgeclient.WorkspaceModeHTBLinux && workspace.MachineName == machine {
			return edge.OperationResult{WorkspaceID: workspace.ID, AuthorizationRevision: workspace.AuthorizationRevision}, ""
		}
	}
	return edge.OperationResult{}, "workspace_unavailable"
}

func resolveOperationWorkspace(stateRoot, id string) (edge.OperationResult, string) {
	registry, err := edgeclient.OpenWorkspaceRegistry(stateRoot)
	if err != nil {
		return edge.OperationResult{}, "workspace_unavailable"
	}
	defer registry.Close()
	workspace, err := registry.Get(id)
	if err != nil {
		return edge.OperationResult{}, "workspace_unavailable"
	}
	return edge.OperationResult{WorkspaceID: workspace.ID, AuthorizationRevision: workspace.AuthorizationRevision}, ""
}

func safeControlFailure(err error) string {
	if err == nil {
		return "none"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "vpn") || strings.Contains(message, "route"):
		return "vpn_unavailable"
	case strings.Contains(message, "target"):
		return "target_invalid"
	case strings.Contains(message, "inventory") || strings.Contains(message, "tool"):
		return "tools_unavailable"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "operation_failed"
	}
}

func waitControlOperation(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
