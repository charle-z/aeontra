package development

import "regexp"

// WorkspaceAnchor captures administrative identity, not a mutable Git status.
// Reassigning an alias cannot retarget an existing objective.
type WorkspaceAnchor struct {
	DeviceID    string `json:"device_id"`
	WorkspaceID string `json:"workspace_id"`
	Generation  uint64 `json:"generation"`
	Owner       string `json:"owner"`
	Repository  string `json:"repository"`
}

var (
	anchorDevicePattern     = regexp.MustCompile(`^ed_[a-f0-9]{32}$`)
	anchorWorkspacePattern  = regexp.MustCompile(`^ws_[a-f0-9]{32}$`)
	anchorOwnerPattern      = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	anchorRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
)

func (anchor WorkspaceAnchor) Valid() bool {
	return anchorDevicePattern.MatchString(anchor.DeviceID) && anchorWorkspacePattern.MatchString(anchor.WorkspaceID) &&
		anchor.Generation > 0 && anchor.Generation <= 1<<63-1 && anchorOwnerPattern.MatchString(anchor.Owner) &&
		anchorRepositoryPattern.MatchString(anchor.Repository)
}

func (scope ObjectiveScope) Pinned() bool { return scope.Bound() && scope.Anchor.Valid() }
