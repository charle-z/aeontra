package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type labPrepareParams struct {
	DeviceID        string `json:"device_id"`
	Platform        string `json:"platform"`
	Machine         string `json:"machine"`
	Target          string `json:"target"`
	Difficulty      string `json:"difficulty"`
	OperatingSystem string `json:"operating_system"`
}

type labRetargetParams struct {
	WorkspaceID string `json:"workspace_id"`
	Target      string `json:"target"`
}

type edgeOperationPublicView struct {
	OperationID           string              `json:"operation_id"`
	State                 edge.OperationState `json:"state"`
	WorkspaceID           string              `json:"workspace_id,omitempty"`
	AuthorizationRevision uint64              `json:"authorization_revision,omitempty"`
	SafeCode              string              `json:"safe_code,omitempty"`
	JobID                 string              `json:"job_id,omitempty"`
	JobState              string              `json:"job_state,omitempty"`
	ProgressRevision      uint64              `json:"progress_revision,omitempty"`
	CycleCount            uint64              `json:"cycle_count,omitempty"`
	JobSafeCode           string              `json:"job_safe_code,omitempty"`
}

func (s *Server) addEdgeControlTools() {
	writeHints := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	s.addDirectTool(toolDef{
		Name: "workspace_lab_prepare", Description: "Create or reuse one authorized HTB Linux workspace on a paired Edge using only closed lab metadata; execution remains local.",
		InputSchema: closedObject(map[string]any{
			"device_id":        stringSchema("opaque active Edge device id", `^ed_[a-f0-9]{32}$`, 35),
			"platform":         map[string]any{"type": "string", "enum": []string{"htb"}},
			"machine":          stringSchema("authorized HTB machine name", `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`, 64),
			"target":           stringSchema("one private target IPv4", `^[0-9.]{7,15}$`, 15),
			"difficulty":       map[string]any{"type": "string", "enum": []string{"easy", "medium", "hard"}},
			"operating_system": map[string]any{"type": "string", "enum": []string{"linux"}},
		}, []string{"device_id", "platform", "machine", "target", "difficulty", "operating_system"}), Version: "1", Annotations: writeHints,
	}, s.handleLabPrepare)
	s.addDirectTool(toolDef{
		Name: "workspace_lab_retarget", Description: "Rotate one authorized HTB workspace to a new private target after the Edge validates its VPN route; prior sessions are invalidated locally.",
		InputSchema: closedObject(map[string]any{
			"workspace_id": stringSchema("opaque authorized workspace id", `^ws_[a-f0-9]{32}$`, 35),
			"target":       stringSchema("one private target IPv4", `^[0-9.]{7,15}$`, 15),
		}, []string{"workspace_id", "target"}), Version: "1", Annotations: writeHints,
	}, s.handleLabRetarget)
	for _, definition := range []struct {
		name, description string
		kind              edge.OperationKind
		destructive       bool
	}{{"workspace_autopilot_start", "Start or reuse one durable local autopilot job for an authorized workspace.", edge.OperationAutopilotStart, false}, {"workspace_autopilot_pause", "Pause one durable local autopilot job after its current bounded cycle.", edge.OperationAutopilotPause, false}, {"workspace_autopilot_resume", "Resume one paused or safely blocked local autopilot job without losing checkpoint or evidence.", edge.OperationAutopilotResume, false}, {"workspace_autopilot_cancel", "Cancel one durable local autopilot job and prevent new cycles.", edge.OperationAutopilotCancel, true}} {
		definition := definition
		schema := map[string]any{"workspace_id": stringSchema("opaque authorized workspace id", `^ws_[a-f0-9]{32}$`, 35)}
		required := []string{"workspace_id"}
		if definition.kind == edge.OperationAutopilotStart {
			schema["run_until"] = map[string]any{"type": "string", "enum": []string{"completed_or_cancelled"}}
			required = append(required, "run_until")
		}
		hints := writeHints
		if definition.destructive {
			hints = map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false}
		}
		s.addDirectTool(toolDef{Name: definition.name, Description: definition.description, InputSchema: closedObject(schema, required), Version: "1", Annotations: hints}, func(arguments json.RawMessage) (string, error) {
			return s.handleAutopilotControl(arguments, definition.kind)
		})
	}
	s.addDirectTool(toolDef{Name: "workspace_autopilot_status", Description: "Return only durable non-sensitive state and progress metadata for one local autopilot job.", InputSchema: closedObject(map[string]any{"workspace_id": stringSchema("opaque authorized workspace id", `^ws_[a-f0-9]{32}$`, 35)}, []string{"workspace_id"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}, s.handleAutopilotStatus)
}

type autopilotControlParams struct {
	WorkspaceID string `json:"workspace_id"`
	RunUntil    string `json:"run_until,omitempty"`
}

func (s *Server) handleAutopilotControl(arguments json.RawMessage, kind edge.OperationKind) (string, error) {
	if s.edgeOperations == nil || s.edgeWorkspaces == nil {
		return "", errEdgeStoreUnavailable
	}
	var params autopilotControlParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	binding, err := s.edgeWorkspaces.ResolveWorkspace(params.WorkspaceID)
	if err != nil {
		return "", err
	}
	if binding.Profile != "linux-workcell" || binding.Mode != "htb-linux" {
		return "", errors.New("registered workspace is not an authorized htb-linux workcell")
	}
	op, _, err := s.edgeOperations.CreateOperation(binding.DeviceID, kind, edge.OperationRequest{WorkspaceID: params.WorkspaceID, RunUntil: params.RunUntil})
	if err == nil {
		op, err = s.edgeOperations.WaitOperation(context.Background(), op.ID, 120*time.Second)
	}
	return marshalToolValue(publicEdgeOperation(op), err)
}
func (s *Server) handleAutopilotStatus(arguments json.RawMessage) (string, error) {
	if s.edgeOperations == nil {
		return "", errEdgeStoreUnavailable
	}
	var params htbWorkspaceParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	result, err := s.edgeOperations.AutopilotStatus(params.WorkspaceID)
	return marshalToolValue(edgeOperationPublicView{WorkspaceID: result.WorkspaceID, JobID: result.JobID, JobState: result.JobState, ProgressRevision: result.ProgressRevision, CycleCount: result.CycleCount, JobSafeCode: result.JobSafeCode}, err)
}

func (s *Server) handleLabPrepare(arguments json.RawMessage) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	var params labPrepareParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	op, _, err := s.edgeOperations.CreateOperation(params.DeviceID, edge.OperationLabPrepare, edge.OperationRequest{Platform: params.Platform, Machine: params.Machine, Target: params.Target, Difficulty: params.Difficulty, OperatingSystem: params.OperatingSystem})
	if err == nil {
		op, err = s.edgeOperations.WaitOperation(context.Background(), op.ID, 120*time.Second)
	}
	return marshalToolValue(publicEdgeOperation(op), err)
}

func (s *Server) handleLabRetarget(arguments json.RawMessage) (string, error) {
	if s.edgeOperations == nil || s.edgeWorkspaces == nil {
		return "", errEdgeStoreUnavailable
	}
	var params labRetargetParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	binding, err := s.edgeWorkspaces.ResolveWorkspace(params.WorkspaceID)
	if err != nil {
		return "", err
	}
	if binding.Profile != "linux-workcell" || binding.Mode != "htb-linux" {
		return "", errors.New("registered workspace is not an authorized htb-linux workcell")
	}
	op, _, err := s.edgeOperations.CreateOperation(binding.DeviceID, edge.OperationLabRetarget, edge.OperationRequest{WorkspaceID: params.WorkspaceID, Target: params.Target})
	if err == nil {
		op, err = s.edgeOperations.WaitOperation(context.Background(), op.ID, 120*time.Second)
	}
	return marshalToolValue(publicEdgeOperation(op), err)
}

func publicEdgeOperation(op edge.Operation) edgeOperationPublicView {
	return edgeOperationPublicView{OperationID: op.ID, State: op.State, WorkspaceID: op.Result.WorkspaceID, AuthorizationRevision: op.Result.AuthorizationRevision, SafeCode: op.SafeCode, JobID: op.Result.JobID, JobState: op.Result.JobState, ProgressRevision: op.Result.ProgressRevision, CycleCount: op.Result.CycleCount, JobSafeCode: op.Result.JobSafeCode}
}
