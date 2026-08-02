package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectExecParams struct {
	Alias          string            `json:"alias"`
	Target         string            `json:"target"`
	IdempotencyKey string            `json:"idempotency_key"`
	Argv           []string          `json:"argv"`
	CWD            string            `json:"cwd,omitempty"`
	Stdin          string            `json:"stdin,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds"`
}

type projectExecPublicView struct {
	OperationID     string              `json:"operation_id"`
	State           edge.OperationState `json:"state"`
	Alias           string              `json:"alias"`
	Repository      string              `json:"repository,omitempty"`
	Target          string              `json:"target"`
	Profile         string              `json:"profile,omitempty"`
	Mode            string              `json:"mode,omitempty"`
	Completed       bool                `json:"completed"`
	ExitCode        int                 `json:"exit_code"`
	Stdout          string              `json:"stdout,omitempty"`
	Stderr          string              `json:"stderr,omitempty"`
	TimedOut        bool                `json:"timed_out"`
	StdoutTruncated bool                `json:"stdout_truncated"`
	StderrTruncated bool                `json:"stderr_truncated"`
	Reused          bool                `json:"reused"`
	Reason          string              `json:"reason,omitempty"`
}

func (s *Server) addProjectExecTool(projectSchema map[string]any) {
	s.addDirectTool(toolDef{
		Name:        "project_exec",
		Description: "Execute one bounded foreground argv inside the selected trusted Edge development workcell. The command runs through Bubblewrap with workspace-only writable state, a clean environment, bounded redacted output, durable idempotency, cancellation and a maximum timeout of 120 seconds. No implicit shell is added.",
		InputSchema: closedObject(map[string]any{
			"alias":           projectSchema["alias"],
			"target":          projectSchema["target"],
			"idempotency_key": stringSchema("caller-generated durable operation key", `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`, 128),
			"argv": map[string]any{
				"type": "array", "minItems": 1, "maxItems": 128,
				"items": map[string]any{"type": "string", "minLength": 1, "maxLength": 8192},
			},
			"cwd":   map[string]any{"type": "string", "maxLength": 1024},
			"stdin": map[string]any{"type": "string", "maxLength": edge.MaxProjectExecStdinBytes},
			"environment": map[string]any{
				"type": "object", "maxProperties": 32,
				"propertyNames":        map[string]any{"pattern": `^[A-Za-z_][A-Za-z0-9_]{0,63}$`},
				"additionalProperties": map[string]any{"type": "string", "maxLength": 4096},
			},
			"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 120},
		}, []string{"alias", "target", "idempotency_key", "argv", "timeout_seconds"}),
		Version: "1",
		Annotations: map[string]any{
			"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": true,
		},
	}, s.handleProjectExec)
}

func (s *Server) handleProjectExec(arguments json.RawMessage) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	var params projectExecParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	device, err := resolver.ResolveActiveDeviceName(params.Target)
	if err != nil {
		return "", err
	}
	request := edge.OperationRequest{
		Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell",
		IdempotencyKey: params.IdempotencyKey, Argv: params.Argv, CWD: params.CWD,
		Stdin: params.Stdin, Environment: params.Environment, TimeoutSeconds: params.TimeoutSeconds,
	}
	operation, created, err := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectExec, request)
	if err == nil {
		operation, err = s.edgeOperations.WaitOperation(context.Background(), operation.ID, 180*time.Second)
	}
	view := projectExecPublicView{
		OperationID: operation.ID, State: operation.State, Alias: params.Alias, Target: params.Target,
		ExitCode: operation.Result.ExecExitCode, TimedOut: operation.Result.ExecTimedOut,
		StdoutTruncated: operation.Result.ExecStdoutTruncated, StderrTruncated: operation.Result.ExecStderrTruncated,
		Reused: err == nil && !created,
	}
	if operation.State == edge.OperationSucceeded {
		view.Alias = operation.Result.ProjectAlias
		view.Repository = operation.Result.ProjectOwner + "/" + operation.Result.ProjectRepository
		view.Target = operation.Result.ProjectTarget
		view.Profile = operation.Result.ProjectProfile
		view.Mode = operation.Result.ProjectMode
		view.Completed = operation.Result.ExecCompleted
		view.Stdout = operation.Result.ExecStdout
		view.Stderr = operation.Result.ExecStderr
	} else if operation.State == edge.OperationFailed || operation.State == edge.OperationCancelled {
		view.Reason = operation.SafeCode
	}
	return marshalToolValue(view, err)
}
