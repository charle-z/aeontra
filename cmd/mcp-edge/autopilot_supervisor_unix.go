//go:build !windows

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/charle-z/mcp-devbox/internal/autopilot"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func runAutopilotSupervisor(ctx context.Context, stateRoot, bundleRoot string, transport *edgeclient.Transport, stderr io.Writer) {
	worker := filepath.Join(filepath.Clean(bundleRoot), "libexec", "mcp-autopilot-worker")
	info, err := os.Stat(worker)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
		fmt.Fprintln(stderr, "mcp-edge: autopilot worker unavailable")
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		registry, err := edgeclient.OpenWorkspaceRegistry(stateRoot)
		if err != nil {
			waitControlOperation(ctx, 5*time.Second)
			continue
		}
		workspaces, listErr := registry.List()
		_ = registry.Close()
		if listErr != nil {
			waitControlOperation(ctx, 5*time.Second)
			continue
		}
		worked := false
		for _, workspace := range workspaces {
			if workspace.Mode != edgeclient.WorkspaceModeHTBLinux {
				continue
			}
			job, loadErr := (autopilot.Store{Workspace: workspace.Path}).Load()
			if loadErr != nil || job.State != autopilot.StateRunning {
				continue
			}
			worked = true
			cycleCtx, cancel := context.WithTimeout(ctx, 11*time.Minute)
			command := exec.CommandContext(cycleCtx, worker, "--state", stateRoot, "--workspace-id", workspace.ID)
			command.Env = []string{"HOME=" + filepath.Dir(filepath.Dir(workspace.Path)), "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
			command.Stdout = io.Discard
			command.Stderr = io.Discard
			runErr := command.Run()
			cancel()
			if runErr != nil {
				fmt.Fprintln(stderr, "mcp-edge: autopilot cycle failed safely")
			}
			updated, loadErr := (autopilot.Store{Workspace: workspace.Path}).Load()
			if loadErr == nil {
				reportCtx, reportCancel := context.WithTimeout(ctx, 20*time.Second)
				reportErr := transport.ReportAutopilot(reportCtx, edge.OperationResult{WorkspaceID: workspace.ID, JobID: updated.JobID, JobState: string(updated.State), ProgressRevision: updated.ProgressRevision, CycleCount: updated.CycleCount, JobSafeCode: updated.SafeCode})
				reportCancel()
				if reportErr != nil {
					fmt.Fprintln(stderr, "mcp-edge: autopilot status report failed safely")
				}
			}
			if ctx.Err() != nil {
				return
			}
		}
		delay := 5 * time.Second
		if worked {
			delay = time.Second
		}
		if !waitControlOperation(ctx, delay) {
			return
		}
	}
}
