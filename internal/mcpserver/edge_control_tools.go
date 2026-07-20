package mcpserver

import (
	"encoding/json"
	"errors"

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
	return marshalToolValue(publicEdgeOperation(op), err)
}

func publicEdgeOperation(op edge.Operation) edgeOperationPublicView {
	return edgeOperationPublicView{OperationID: op.ID, State: op.State, WorkspaceID: op.Result.WorkspaceID, AuthorizationRevision: op.Result.AuthorizationRevision, SafeCode: op.SafeCode}
}
