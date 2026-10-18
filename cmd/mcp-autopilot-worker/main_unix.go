//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/charle-z/mcp-devbox/internal/autopilot"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type modelConfig struct {
	Version  int    `json:"version"`
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint"`
}
type safeResult struct {
	JobID            string             `json:"job_id"`
	WorkspaceID      string             `json:"workspace_id"`
	State            autopilot.JobState `json:"state"`
	ProgressRevision uint64             `json:"progress_revision"`
	CycleCount       uint64             `json:"cycle_count"`
	SafeCode         string             `json:"safe_code,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-autopilot-worker: cycle failed safely")
		os.Exit(1)
	}
}
func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mcp-autopilot-worker", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateRoot := fs.String("state", "", "private Edge state root")
	workspaceID := fs.String("workspace-id", "", "opaque workspace id")
	modelPath := fs.String("model-config", "/etc/mcp-devbox/autopilot-model.json", "local model configuration")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New("worker accepts only closed cycle options")
	}
	if !filepath.IsAbs(*stateRoot) || !filepath.IsAbs(*modelPath) || os.Geteuid() == 0 {
		return errors.New("worker configuration is unsafe")
	}
	registry, err := edgeclient.OpenWorkspaceRegistry(*stateRoot)
	if err != nil {
		return err
	}
	workspace, err := registry.Get(*workspaceID)
	_ = registry.Close()
	if err != nil || workspace.Mode != edgeclient.WorkspaceModeHTBLinux {
		return errors.New("authorized workspace unavailable")
	}
	job, err := (autopilot.Store{Workspace: workspace.Path}).Load()
	if err != nil {
		return err
	}
	if job.State != autopilot.StateRunning {
		return json.NewEncoder(stdout).Encode(publicJob(job))
	}
	config, err := loadModelConfig(*modelPath)
	if err != nil {
		return err
	}
	model := autopilot.LocalHTTPModel{Endpoint: config.Endpoint}
	runtimeRoot, err := os.MkdirTemp(*stateRoot, "autopilot-cycle-")
	if err != nil {
		return errors.New("cycle runtime unavailable")
	}
	defer os.RemoveAll(runtimeRoot)
	if os.Chmod(runtimeRoot, 0o700) != nil {
		return errors.New("cycle runtime unsafe")
	}
	cycleCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	socket := filepath.Join(runtimeRoot, edgeclient.HTBLabBrokerSocketName)
	brokerDone, err := edgeclient.StartHTBLabBroker(cycleCtx, edgeclient.HTBLabBrokerConfig{SocketPath: socket, StateRoot: *stateRoot, Workspace: workspace, RuntimeID: job.JobID, ExpiresAt: time.Now().UTC().Add(10 * time.Minute)})
	if err != nil {
		return err
	}
	defer func() { cancel(); <-brokerDone }()
	bridge := autopilot.BrokerExecutor{SocketPath: socket, Workspace: workspace.Path, WorkspaceID: workspace.ID}
	updated, err := (autopilot.CycleRunner{Store: autopilot.Store{Workspace: workspace.Path}, Model: model, Executor: bridge, Authorization: bridge, ModelTimeout: 2 * time.Minute, ActionTimeout: 5 * time.Minute, StorageLimit: 1 << 30}).Run(cycleCtx)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(publicJob(updated))
}
func loadModelConfig(path string) (modelConfig, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || info.Size() <= 0 || info.Size() > 4096 {
		return modelConfig{}, errors.New("local model configuration is unsafe")
	}
	file, err := os.Open(path)
	if err != nil {
		return modelConfig{}, errors.New("local model configuration unavailable")
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	var config modelConfig
	if decoder.Decode(&config) != nil || decoder.Decode(&struct{}{}) != io.EOF || config.Version != 1 || (config.Provider != "local-http" && config.Provider != "opencode-local") {
		return modelConfig{}, errors.New("local model configuration is invalid")
	}
	return config, nil
}
func publicJob(job autopilot.State) safeResult {
	return safeResult{JobID: job.JobID, WorkspaceID: job.WorkspaceID, State: job.State, ProgressRevision: job.ProgressRevision, CycleCount: job.CycleCount, SafeCode: job.SafeCode}
}
