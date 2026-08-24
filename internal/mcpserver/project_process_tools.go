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

type projectProcessStdinParams struct {
	Alias          string `json:"alias"`
	Target         string `json:"target"`
	ProcessID      string `json:"process_id"`
	IdempotencyKey string `json:"idempotency_key"`
	ExpectedOffset int64  `json:"expected_offset"`
	Data           string `json:"data"`
	CloseStdin     bool   `json:"close_stdin"`
}

type projectProcessStopParams struct {
	Alias        string `json:"alias"`
	Target       string `json:"target"`
	ProcessID    string `json:"process_id"`
	GraceSeconds int    `json:"grace_seconds"`
}

type projectProcessSignalParams struct {
	Alias     string `json:"alias"`
	Target    string `json:"target"`
	ProcessID string `json:"process_id"`
	Signal    string `json:"signal"`
}

type projectProcessListParams struct {
	Alias  string `json:"alias"`
	Target string `json:"target"`
	Limit  int    `json:"limit"`
}

type projectProcessCleanupParams struct {
	Alias     string `json:"alias"`
	Target    string `json:"target"`
	ProcessID string `json:"process_id,omitempty"`
}

type projectProcessPublicView struct {
	OperationID     string                          `json:"operation_id"`
	OperationState  edge.OperationState             `json:"operation_state"`
	ProcessID       string                          `json:"process_id,omitempty"`
	ProcessState    string                          `json:"process_state,omitempty"`
	Alias           string                          `json:"alias"`
	Repository      string                          `json:"repository,omitempty"`
	Target          string                          `json:"target"`
	Profile         string                          `json:"profile,omitempty"`
	Mode            string                          `json:"mode,omitempty"`
	StartedAt       string                          `json:"started_at,omitempty"`
	FinishedAt      string                          `json:"finished_at,omitempty"`
	ExitKnown       bool                            `json:"exit_known"`
	ExitCode        int                             `json:"exit_code"`
	TerminalSignal  string                          `json:"terminal_signal,omitempty"`
	Stdout          string                          `json:"stdout,omitempty"`
	Stderr          string                          `json:"stderr,omitempty"`
	StdoutNext      int64                           `json:"stdout_next"`
	StderrNext      int64                           `json:"stderr_next"`
	StdoutEOF       bool                            `json:"stdout_eof"`
	StderrEOF       bool                            `json:"stderr_eof"`
	StdoutTruncated bool                            `json:"stdout_truncated"`
	StderrTruncated bool                            `json:"stderr_truncated"`
	StdinNext       int64                           `json:"stdin_next"`
	StdinAccepted   int                             `json:"stdin_accepted"`
	StdinClosed     bool                            `json:"stdin_closed"`
	Reused          bool                            `json:"reused"`
	Reason          string                          `json:"reason,omitempty"`
	Processes       []edge.BackgroundProcessSummary `json:"processes,omitempty"`
	Removed         int                             `json:"removed,omitempty"`
	Active          int                             `json:"active,omitempty"`
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
		Name: "project_process_stdin", Description: "Write one ordered bounded UTF-8 chunk to the stdin of an owned durable project process, or close stdin explicitly. The expected byte offset and idempotency key make retries safe; content, PID and local paths are never returned.",
		InputSchema: closedObject(map[string]any{
			"alias": projectSchema["alias"], "target": projectSchema["target"], "process_id": processID,
			"idempotency_key": stringSchema("caller-generated stdin write key", `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`, 128),
			"expected_offset": map[string]any{"type": "integer", "minimum": 0, "maximum": edge.MaxProjectProcessStdinTotalBytes},
			"data":            map[string]any{"type": "string", "maxLength": edge.MaxProjectProcessStdinBytes},
			"close_stdin":     map[string]any{"type": "boolean"},
		}, []string{"alias", "target", "process_id", "idempotency_key", "expected_offset", "data", "close_stdin"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": true},
	}, s.handleProjectProcessStdin)
	s.addDirectTool(toolDef{
		Name: "project_process_stop", Description: "Idempotently stop one owned durable project process by opaque id. The Edge revalidates process identity, sends TERM to only its process group, waits a finite grace period, and escalates to KILL only when needed.",
		InputSchema: closedObject(map[string]any{
			"alias": projectSchema["alias"], "target": projectSchema["target"], "process_id": processID,
			"grace_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 30},
		}, []string{"alias", "target", "process_id", "grace_seconds"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false},
	}, s.handleProjectProcessStop)
	s.addDirectTool(toolDef{
		Name: "project_process_signal", Description: "Send one closed, workspace-owned signal (interrupt, terminate, or kill) to a durable project process group after PID/start-time identity revalidation. Arbitrary signal numbers and host-wide targets are rejected.",
		InputSchema: closedObject(map[string]any{
			"alias": projectSchema["alias"], "target": projectSchema["target"], "process_id": processID,
			"signal": map[string]any{"type": "string", "enum": []string{"interrupt", "terminate", "kill"}},
		}, []string{"alias", "target", "process_id", "signal"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": false, "openWorldHint": false},
	}, s.handleProjectProcessSignal)
	s.addDirectTool(toolDef{
		Name: "project_process_list", Description: "List bounded durable process metadata for one project and Edge target. Results expose only opaque ids, lifecycle states, timestamps and safe terminal metadata; never PID, argv, environment or paths.",
		InputSchema: closedObject(map[string]any{
			"alias": projectSchema["alias"], "target": projectSchema["target"], "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		}, []string{"alias", "target", "limit"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
	}, s.handleProjectProcessList)
	s.addDirectTool(toolDef{
		Name: "project_process_cleanup", Description: "Explicitly remove terminal durable process metadata and private logs for one process or all terminal processes in one project/target. Live processes and their logs are preserved.",
		InputSchema: closedObject(map[string]any{
			"alias": projectSchema["alias"], "target": projectSchema["target"], "process_id": processID,
		}, []string{"alias", "target"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false},
	}, s.handleProjectProcessCleanup)
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

func (s *Server) handleProjectProcessStdin(arguments json.RawMessage) (string, error) {
	var params projectProcessStdinParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	return s.runProjectProcessOperation(params.Target, edge.OperationProjectProcessStdin, edge.OperationRequest{
		Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell", BackgroundProcessID: params.ProcessID,
		IdempotencyKey: params.IdempotencyKey, ProcessStdinOffset: params.ExpectedOffset, Stdin: params.Data, ProcessStdinClose: params.CloseStdin,
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

func (s *Server) handleProjectProcessSignal(arguments json.RawMessage) (string, error) {
	var params projectProcessSignalParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	return s.runProjectProcessOperation(params.Target, edge.OperationProjectProcessSignal, edge.OperationRequest{
		Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell", BackgroundProcessID: params.ProcessID, BackgroundSignal: params.Signal,
	}, params.Alias, params.Target)
}

func (s *Server) handleProjectProcessList(arguments json.RawMessage) (string, error) {
	var params projectProcessListParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	return s.runProjectProcessOperation(params.Target, edge.OperationProjectProcessList, edge.OperationRequest{
		Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell", ProcessLimit: params.Limit,
	}, params.Alias, params.Target)
}

func (s *Server) handleProjectProcessCleanup(arguments json.RawMessage) (string, error) {
	var params projectProcessCleanupParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	return s.runProjectProcessOperation(params.Target, edge.OperationProjectProcessCleanup, edge.OperationRequest{
		Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell", BackgroundProcessID: params.ProcessID,
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
		StdinNext: result.BackgroundStdinNext, StdinAccepted: result.BackgroundStdinAccepted, StdinClosed: result.BackgroundStdinClosed,
		Reused:    err == nil && (!created || result.BackgroundStdinReused),
		Processes: result.BackgroundProcesses, Removed: result.BackgroundCleanupRemoved, Active: result.BackgroundCleanupActive,
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
