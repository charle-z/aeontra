package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectToolboxParams struct {
	Alias          string            `json:"alias"`
	Target         string            `json:"target"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Argv           []string          `json:"argv,omitempty"`
	CWD            string            `json:"cwd,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
}

type projectToolboxPublicView struct {
	OperationID     string              `json:"operation_id"`
	OperationState  edge.OperationState `json:"operation_state"`
	Alias           string              `json:"alias"`
	Repository      string              `json:"repository,omitempty"`
	Target          string              `json:"target"`
	ToolboxID       string              `json:"toolbox_id,omitempty"`
	ToolboxState    string              `json:"toolbox_state,omitempty"`
	Base            string              `json:"base,omitempty"`
	BaseImageID     string              `json:"base_image_id,omitempty"`
	CreatedAt       string              `json:"created_at,omitempty"`
	UpdatedAt       string              `json:"updated_at,omitempty"`
	Output          string              `json:"output,omitempty"`
	OutputTruncated bool                `json:"output_truncated"`
	Removed         bool                `json:"removed"`
	Reused          bool                `json:"reused"`
	Reason          string              `json:"reason,omitempty"`
}

func (s *Server) addProjectToolboxTools(projectSchema map[string]any) {
	common := map[string]any{"alias": projectSchema["alias"], "target": projectSchema["target"]}
	key := stringSchema("caller-generated durable operation key", `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`, 128)
	argv := map[string]any{"type": "array", "minItems": 1, "maxItems": 128, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 8192}}
	environment := map[string]any{"type": "object", "maxProperties": 32, "propertyNames": map[string]any{"pattern": `^[A-Za-z_][A-Za-z0-9_]{0,63}$`}, "additionalProperties": map[string]any{"type": "string", "maxLength": 4096}}
	execProperties := map[string]any{"alias": common["alias"], "target": common["target"], "idempotency_key": key, "argv": argv, "cwd": map[string]any{"type": "string", "maxLength": 1024}, "environment": environment, "timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 3600}}
	s.addDirectTool(toolDef{Name: "project_toolbox_create", Description: "Create or recover the project's persistent rootless Debian toolbox. The server owns the base image and container identity; packages, caches and writable rootfs remain until explicit cleanup.", InputSchema: closedObject(map[string]any{"alias": common["alias"], "target": common["target"], "idempotency_key": key}, []string{"alias", "target", "idempotency_key"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": true}}, func(raw json.RawMessage) (string, error) {
		return s.handleProjectToolbox(raw, edge.OperationProjectToolboxCreate)
	})
	s.addDirectTool(toolDef{Name: "project_toolbox_status", Description: "Inspect the registered project's persistent rootless toolbox without exposing host paths, engine names or container identifiers.", InputSchema: closedObject(common, []string{"alias", "target"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}}, func(raw json.RawMessage) (string, error) {
		return s.handleProjectToolbox(raw, edge.OperationProjectToolboxStatus)
	})
	for _, definition := range []struct {
		name, description string
		kind              edge.OperationKind
	}{
		{name: "project_toolbox_exec", description: "Execute arbitrary argv inside the persistent rootless toolbox with the project mounted at /workspace. No implicit shell or command allowlist is added.", kind: edge.OperationProjectToolboxExec},
		{name: "project_toolbox_install", description: "Install toolchains, system packages or project dependencies by executing explicit argv as root inside the rootless toolbox; the host package database is not modified.", kind: edge.OperationProjectToolboxInstall},
	} {
		definition := definition
		s.addDirectTool(toolDef{Name: definition.name, Description: definition.description, InputSchema: closedObject(execProperties, []string{"alias", "target", "idempotency_key", "argv", "timeout_seconds"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": true}}, func(raw json.RawMessage) (string, error) {
			return s.handleProjectToolbox(raw, definition.kind)
		})
	}
	s.addDirectTool(toolDef{Name: "project_toolbox_cleanup", Description: "Explicitly remove the registered project's persistent toolbox rootfs. It never runs automatically and does not delete the project workspace.", InputSchema: closedObject(map[string]any{"alias": common["alias"], "target": common["target"], "idempotency_key": key}, []string{"alias", "target", "idempotency_key"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": true}}, func(raw json.RawMessage) (string, error) {
		return s.handleProjectToolbox(raw, edge.OperationProjectToolboxCleanup)
	})
}

func (s *Server) handleProjectToolbox(arguments json.RawMessage, kind edge.OperationKind) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	var params projectToolboxParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	device, err := resolver.ResolveActiveDeviceName(params.Target)
	if err != nil {
		return "", err
	}
	request := edge.OperationRequest{Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell", IdempotencyKey: params.IdempotencyKey, Argv: params.Argv, CWD: params.CWD, Environment: params.Environment, TimeoutSeconds: params.TimeoutSeconds}
	operation, created, err := s.edgeOperations.CreateOperation(device.ID, kind, request)
	if err == nil {
		operation, err = s.edgeOperations.WaitOperation(context.Background(), operation.ID, 180*time.Second)
	}
	view := projectToolboxPublicView{OperationID: operation.ID, OperationState: operation.State, Alias: params.Alias, Target: params.Target, Reused: err == nil && !created}
	if operation.State == edge.OperationSucceeded {
		result := operation.Result
		view.Alias, view.Repository, view.Target = result.ProjectAlias, result.ProjectOwner+"/"+result.ProjectRepository, result.ProjectTarget
		view.ToolboxID, view.ToolboxState, view.Base, view.BaseImageID = result.ToolboxID, result.ToolboxState, result.ToolboxBase, result.ToolboxBaseImageID
		view.CreatedAt, view.UpdatedAt, view.Output = result.ToolboxCreatedAt, result.ToolboxUpdatedAt, result.ToolboxOutput
		view.OutputTruncated, view.Removed = result.ToolboxOutputTruncated, result.ToolboxRemoved
	} else if operation.State == edge.OperationFailed || operation.State == edge.OperationCancelled {
		view.Reason = operation.SafeCode
	}
	return marshalToolValue(view, err)
}
