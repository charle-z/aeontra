package mcpserver

import (
	"encoding/json"
	"errors"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/htbaction"
)

var errHTBActionRuntimeRequired = errors.New("authorized HTB actions execute only inside an active htb-linux workspace runtime")

type htbWorkspaceParams struct {
	WorkspaceID string `json:"workspace_id"`
}

type htbCredentialParams struct {
	Source       string `json:"source"`
	ExtractAfter string `json:"extract_after"`
}

type htbAuthValidateParams struct {
	WorkspaceID    string              `json:"workspace_id"`
	Username       string              `json:"username"`
	Credential     htbCredentialParams `json:"credential"`
	TimeoutSeconds int                 `json:"timeout_seconds"`
}

type htbCommandParams struct {
	WorkspaceID    string `json:"workspace_id"`
	SessionID      string `json:"session_id"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type htbCommandSaveParams struct {
	WorkspaceID    string `json:"workspace_id"`
	SessionID      string `json:"session_id"`
	Command        string `json:"command"`
	SaveOutput     string `json:"save_output"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type htbSessionCloseParams struct {
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
}

func (s *Server) addHTBActionTools() {
	definitions := htbaction.Definitions()
	for _, definition := range definitions {
		definition := definition
		hints := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": true}
		switch definition.Name {
		case htbaction.ToolStatus:
			hints = map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
		case htbaction.ToolSessionClose:
			hints = map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false}
		}
		s.addDirectTool(toolDef{
			Name: definition.Name, Description: definition.Description, InputSchema: definition.InputSchema,
			Version: "1", Annotations: hints,
		}, func(arguments json.RawMessage) (string, error) {
			workspaceID, err := decodeHTBActionWorkspace(definition.Name, arguments)
			if err != nil {
				return "", err
			}
			if err := s.requireAuthorizedHTBWorkspace(workspaceID); err != nil {
				return "", err
			}
			return "", errHTBActionRuntimeRequired
		})
	}
}

func decodeHTBActionWorkspace(name string, arguments json.RawMessage) (string, error) {
	switch name {
	case htbaction.ToolStatus:
		var params htbWorkspaceParams
		if err := decodeClosed(arguments, &params); err != nil {
			return "", err
		}
		return params.WorkspaceID, nil
	case htbaction.ToolAuthValidate:
		var params htbAuthValidateParams
		if err := decodeClosed(arguments, &params); err != nil {
			return "", err
		}
		return params.WorkspaceID, nil
	case htbaction.ToolCommand, htbaction.ToolCommandCredentialStdin:
		var params htbCommandParams
		if err := decodeClosed(arguments, &params); err != nil {
			return "", err
		}
		return params.WorkspaceID, nil
	case htbaction.ToolCommandSave:
		var params htbCommandSaveParams
		if err := decodeClosed(arguments, &params); err != nil {
			return "", err
		}
		return params.WorkspaceID, nil
	case htbaction.ToolSessionClose:
		var params htbSessionCloseParams
		if err := decodeClosed(arguments, &params); err != nil {
			return "", err
		}
		return params.WorkspaceID, nil
	default:
		return "", errors.New("unknown HTB action")
	}
}

func (s *Server) requireAuthorizedHTBWorkspace(workspaceID string) error {
	if s.edgeWorkspaces == nil {
		return errWorkspaceRegistryUnavailable
	}
	binding, err := s.edgeWorkspaces.ResolveWorkspace(workspaceID)
	if err != nil {
		return err
	}
	if binding.WorkspaceID != workspaceID || binding.Profile != "linux-workcell" || binding.Mode != "htb-linux" {
		return errors.New("registered workspace is not an authorized htb-linux workcell")
	}
	if s.edgeDevices == nil || !s.edgeDevices.DeviceActive(binding.DeviceID) {
		return errors.New("registered active workspace not found")
	}
	if !validWorkspaceBinding(edge.WorkspaceBinding{
		WorkspaceID: binding.WorkspaceID, DeviceID: binding.DeviceID, Profile: binding.Profile, Mode: binding.Mode,
	}, workspaceID) {
		return errors.New("registered workspace authorization is invalid")
	}
	return nil
}
