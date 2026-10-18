package edgeclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

const workspaceRegistrationPath = "/edge/v1/workspaces/register"

func (t *Transport) RegisterWorkspaces(ctx context.Context, workspaces []Workspace) error {
	if t == nil || len(workspaces) > edge.MaxRegisteredWorkspaces {
		return errors.New("workspace registration is invalid")
	}
	items := make([]edge.WorkspaceRegistration, 0, len(workspaces))
	for _, workspace := range workspaces {
		item := edge.WorkspaceRegistration{
			WorkspaceID: workspace.ID,
			Profile:     string(workspace.Profile),
			Mode:        string(workspace.Mode),
		}
		if item.WorkspaceID == "" || item.Profile == "" || item.Mode == "" {
			return errors.New("workspace registration is invalid")
		}
		items = append(items, item)
	}
	var status edge.WorkspaceRegistrationStatus
	code, err := t.do(ctx, http.MethodPost, workspaceRegistrationPath, map[string]any{"workspaces": items}, &status)
	if err != nil {
		return err
	}
	if code != http.StatusOK || status.Count != len(items) || status.UpdatedAt.IsZero() {
		return fmt.Errorf("workspace registration rejected with HTTP %d", code)
	}
	return nil
}
