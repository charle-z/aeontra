package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectProcessStartParams struct {
	Alias          string            `json:"alias"`
	Target         string            `json:"target"`
	IdempotencyKey string            `json:"idempotency_key"`
	Argv           []string          `json:"argv"`
	CWD            string            `json:"cwd,omitempty"`
	Stdin          string            `json:"stdin,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
}

type projectProcessStatusParams struct {
	Alias        string `json:"alias"`
	Target       string `json:"target"`
	ProcessID    string `json:"process_id"`
	StdoutOffset int64  `json:"stdout_offset"`
	StderrOffset int64  `json:"stderr_offset"`
	LimitBytes   int    `json:"limit_bytes"`
}

type projectProcessStopParams struct {
	Alias        string `json:"alias"`
	Target       string `json:"target"`
	ProcessID    string `json:"process_id"`
	GraceSeconds int    `json:"grace_seconds"`
}

type projectProcessPublicView struct {
	OperationID     string              `json:"operation_id"`
	OperationState  edge.OperationState `json:"operation_state"`
	ProcessID       string              `json:"process_id,omitempty"`
	ProcessState    string              `json:"process_state,omitempty"`
	Alias           string              `json:"alias"`
	Repository      string              `json:"repository,omitempty"`
	Target          string              `json:"target"`
	Profile         string              `json:"profile,omitempty"`
	Mode            string              `json:"mode,omitempty"`
	StartedAt       string              `json:"started_at,omitempty"`
	FinishedAt      string              `json:"finished_at,omitempty"`
	ExitKnown       bool                `json:"exit_known"`
	ExitCode        int                 `json:"exit_code"`
	TerminalSignal  string              `json:"terminal_signal,omitempty"`
	Stdout          string              `json:"stdout,omitempty"`
	Stderr          string              `json:"stderr,omitempty"`
	StdoutNext      int64               `json:"stdout_next"`
	StderrNext      int64               `json:"stderr_next"`
	StdoutEOF       bool                `json:"stdout_eof"`
	StderrEOF       bool                `json:"stderr_eof"`
	StdoutTruncated bool                `json:"stdout_truncated"`
	StderrTruncated bool                `json:"stderr_truncated"`
	Reused          bool                `json:"reused"`
	Reason          string              `json:"reason,omitempty"`
}

func (s *Server) addProjectProcessTools(projectSchema map[string]any) {
	processID := stringSchema("opaque durable project process id", `^pr_[a-f0-9]{32}$`, 35)
	s.addDirectTool(toolDef{
		Name: "project_process_start", Description: "Start or reuse one durable background argv in the same trusted Edge workcell executor used by project_exec. There is no implicit shell, PID or host path in the public contract; logs and metadata remain private and output is read through bounded redacted status calls.",
		InputSchema: closedObject(map[string]any{
			"alias": projectSchema["alias"], "target": projectSchema["target"],
			"idempotency_key": stringSchema("caller-generated durable process key", `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`, 128),
			"argv":            map[string]any{"type": "array", "minItems": 1, "maxItems": 128, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 8192}},
			"cwd":             map[string]any{"type": "string", "maxLength": 1024},
			"stdin":           map[string]any{"type": "string", "maxLength": edge.MaxProjectExecStdinBytes},
			"environment":     map[string]any{"type": "object", "maxProperties": 32, "propertyNames": map[string]any{"pattern": `^[A-Za-z_][A-Za-z0-9_]{0,63}$`}, "additionalProperties": map[string]any{"type": "string", "maxLength": 4096}},
		}, []string{"alias", "target", "idempotency_key", "argv"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": true},
	}, s.handleProjectProcessStart)
	s.addDirectTool(toolDef{
		Name: "project_process_status", Description: "Read one durable project process and bounded incremental redacted stdout/stderr by opaque process id. PID, argv, environment and local paths are never returned.",
		InputSchema: closedObject(map[string]any{
			"alias": projectSchema["alias"], "target": projectSchema["target"], "process_id": processID,
			"stdout_offset": map[string]any{"type": "integer", "minimum": 0}, "stderr_offset": map[string]any{"type": "integer", "minimum": 0},
			"limit_bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": edge.MaxProjectProcessReadBytes},
		}, []string{"alias", "target", "process_id", "stdout_offset", "stderr_offset", "limit_bytes"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
	}, s.handleProjectProcessStatus)
	s.addDirectTool(toolDef{
		Name: "project_process_stop", Description: "Idempotently stop one owned durable project process by opaque id. The Edge revalidates process identity, sends TERM to only its process group, waits a finite grace period, and escalates to KILL only when needed.",
		InputSchema: closedObject(map[string]any{
			"alias": projectSchema["alias"], "target": projectSchema["target"], "process_id": processID,
			"grace_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 30},
		}, []string{"alias", "target", "process_id", "grace_seconds"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false},
	}, s.handleProjectProcessStop)
}

func (s *Server) handleProjectProcessStart(arguments json.RawMessage) (string, error) {
	var params projectProcessStartParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	return s.runProjectProcessOperation(params.Target, edge.OperationProjectProcessStart, edge.OperationRequest{
		Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell", IdempotencyKey: params.IdempotencyKey,
		Argv: params.Argv, CWD: params.CWD, Stdin: params.Stdin, Environment: params.Environment,
	}, params.Alias, params.Target)
}

func (s *Server) handleProjectProcessStatus(arguments json.RawMessage) (string, error) {
	var params projectProcessStatusParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	return s.runProjectProcessOperation(params.Target, edge.OperationProjectProcessStatus, edge.OperationRequest{
		Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell", BackgroundProcessID: params.ProcessID,
		StdoutOffset: params.StdoutOffset, StderrOffset: params.StderrOffset, OutputLimit: params.LimitBytes,
	}, params.Alias, params.Target)
}

func (s *Server) handleProjectProcessStop(arguments json.RawMessage) (string, error) {
	var params projectProcessStopParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	return s.runProjectProcessOperation(params.Target, edge.OperationProjectProcessStop, edge.OperationRequest{
		Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell", BackgroundProcessID: params.ProcessID, GraceSeconds: params.GraceSeconds,
	}, params.Alias, params.Target)
}

func (s *Server) runProjectProcessOperation(target string, kind edge.OperationKind, request edge.OperationRequest, alias, publicTarget string) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	device, err := resolver.ResolveActiveDeviceName(target)
	if err != nil {
		return "", err
	}
	operation, created, err := s.edgeOperations.CreateOperation(device.ID, kind, request)
	if err == nil {
		operation, err = s.edgeOperations.WaitOperation(context.Background(), operation.ID, 90*time.Second)
	}
	result := operation.Result
	view := projectProcessPublicView{
		OperationID: operation.ID, OperationState: operation.State, ProcessID: result.BackgroundProcessID,
		ProcessState: result.BackgroundProcessState, Alias: alias, Target: publicTarget,
		StartedAt: result.BackgroundStartedAt, FinishedAt: result.BackgroundFinishedAt,
		ExitKnown: result.BackgroundExitKnown, ExitCode: result.BackgroundExitCode, TerminalSignal: result.BackgroundTerminalSignal,
		Stdout: result.BackgroundStdout, Stderr: result.BackgroundStderr,
		StdoutNext: result.BackgroundStdoutNext, StderrNext: result.BackgroundStderrNext,
		StdoutEOF: result.BackgroundStdoutEOF, StderrEOF: result.BackgroundStderrEOF,
		StdoutTruncated: result.BackgroundStdoutTruncated, StderrTruncated: result.BackgroundStderrTruncated,
		Reused: err == nil && !created,
	}
	if operation.State == edge.OperationSucceeded {
		view.Alias = result.ProjectAlias
		view.Repository = result.ProjectOwner + "/" + result.ProjectRepository
		view.Target = result.ProjectTarget
		view.Profile = result.ProjectProfile
		view.Mode = result.ProjectMode
		view.Reason = result.BackgroundReason
	} else if operation.State == edge.OperationFailed || operation.State == edge.OperationCancelled {
		view.Reason = operation.SafeCode
	}
	return marshalToolValue(view, err)
}
