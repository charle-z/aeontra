package edge

import (
	"encoding/json"
	"errors"
	"strings"
)

var errRegisteredProjectWorkspaceNotFound = errors.New("registered project workspace not found")

// ResolveProjectWorkspace resolves the most recent successful project binding
// for one active Edge device and human project alias. The operation journal is
// only a locator; the returned workspace must still exist in the current
// signed workspace registration snapshot and remain bound to the same device
// in the development linux-workcell profile.
func (s *Store) ResolveProjectWorkspace(deviceID, alias, target string) (WorkspaceBinding, error) {
	alias = strings.ToLower(strings.TrimSpace(alias))
	target = strings.ToLower(strings.TrimSpace(target))
	if s == nil || s.db == nil || !idPattern.MatchString(deviceID) ||
		!projectOperationAliasPattern.MatchString(alias) ||
		!projectOperationTargetPattern.MatchString(target) {
		return WorkspaceBinding{}, errRegisteredProjectWorkspaceNotFound
	}

	var resultJSON []byte
	if err := s.db.QueryRow(`
SELECT result_json
FROM edge_operations
WHERE device_id = ?
  AND state = ?
  AND result_json IS NOT NULL
  AND json_extract(CAST(request_json AS TEXT), '$.alias') = ?
  AND json_extract(CAST(request_json AS TEXT), '$.target_alias') = ?
ORDER BY updated_at DESC, operation_id DESC
LIMIT 1`, deviceID, OperationSucceeded, alias, target).Scan(&resultJSON); err != nil {
		return WorkspaceBinding{}, errRegisteredProjectWorkspaceNotFound
	}

	var candidate OperationResult
	if json.Unmarshal(resultJSON, &candidate) != nil || candidate.ProjectAlias != alias || candidate.ProjectTarget != target {
		return WorkspaceBinding{}, errRegisteredProjectWorkspaceNotFound
	}
	if candidate.ProjectProfile != "linux-workcell" || candidate.ProjectMode != "dev" ||
		!workspaceIDPattern.MatchString(candidate.WorkspaceID) {
		return WorkspaceBinding{}, errRegisteredProjectWorkspaceNotFound
	}

	binding, err := s.ResolveWorkspace(candidate.WorkspaceID)
	if err != nil || binding.DeviceID != deviceID || binding.Profile != "linux-workcell" || binding.Mode != "dev" {
		return WorkspaceBinding{}, errRegisteredProjectWorkspaceNotFound
	}
	return binding, nil
}
