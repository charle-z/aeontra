package edge

import (
	"database/sql"
	"errors"
	"regexp"
	"time"
)

const MaxRegisteredWorkspaces = 256

var workspaceIDPattern = regexp.MustCompile(`^ws_[a-f0-9]{32}$`)

type WorkspaceRegistration struct {
	WorkspaceID string `json:"workspace_id"`
	Profile     string `json:"profile"`
	Mode        string `json:"mode"`
}

type WorkspaceBinding struct {
	WorkspaceID string    `json:"workspace_id"`
	DeviceID    string    `json:"device_id"`
	Profile     string    `json:"profile"`
	Mode        string    `json:"mode"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type WorkspaceRegistrationStatus struct {
	Count     int       `json:"count"`
	UpdatedAt time.Time `json:"updated_at"`
}

func validWorkspaceRegistration(item WorkspaceRegistration) bool {
	if !workspaceIDPattern.MatchString(item.WorkspaceID) {
		return false
	}
	switch item.Profile {
	case "sandbox":
		return item.Mode == "dev"
	case "linux-workcell":
		return item.Mode == "dev" || item.Mode == "htb-linux"
	default:
		return false
	}
}

func (s *Store) RegisterWorkspaces(deviceID string, workspaces []WorkspaceRegistration) (WorkspaceRegistrationStatus, error) {
	if s == nil || s.db == nil || !idPattern.MatchString(deviceID) || len(workspaces) > MaxRegisteredWorkspaces {
		return WorkspaceRegistrationStatus{}, errors.New("workspace registration is invalid")
	}
	seen := make(map[string]struct{}, len(workspaces))
	for _, workspace := range workspaces {
		if !validWorkspaceRegistration(workspace) {
			return WorkspaceRegistrationStatus{}, errors.New("workspace registration is invalid")
		}
		if _, exists := seen[workspace.WorkspaceID]; exists {
			return WorkspaceRegistrationStatus{}, errors.New("workspace registration is invalid")
		}
		seen[workspace.WorkspaceID] = struct{}{}
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return WorkspaceRegistrationStatus{}, errors.New("workspace registration unavailable")
	}
	defer tx.Rollback()
	var state State
	if err := tx.QueryRow(`SELECT state FROM devices WHERE device_id=?`, deviceID).Scan(&state); err != nil || state != StateActive {
		return WorkspaceRegistrationStatus{}, errors.New("active edge device not found")
	}
	if _, err := tx.Exec(`DELETE FROM edge_workspaces WHERE device_id=?`, deviceID); err != nil {
		return WorkspaceRegistrationStatus{}, errors.New("workspace registration failed")
	}
	for _, workspace := range workspaces {
		var existingDevice string
		err := tx.QueryRow(`SELECT device_id FROM edge_workspaces WHERE workspace_id=?`, workspace.WorkspaceID).Scan(&existingDevice)
		if err == nil && existingDevice != deviceID {
			return WorkspaceRegistrationStatus{}, errors.New("workspace is associated with another edge device")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return WorkspaceRegistrationStatus{}, errors.New("workspace registration failed")
		}
		if _, err := tx.Exec(`INSERT INTO edge_workspaces(workspace_id,device_id,profile,mode,registered_at,updated_at) VALUES(?,?,?,?,?,?)`, workspace.WorkspaceID, deviceID, workspace.Profile, workspace.Mode, now.UnixNano(), now.UnixNano()); err != nil {
			return WorkspaceRegistrationStatus{}, errors.New("workspace registration failed")
		}
	}
	if err := tx.Commit(); err != nil {
		return WorkspaceRegistrationStatus{}, errors.New("workspace registration failed")
	}
	return WorkspaceRegistrationStatus{Count: len(workspaces), UpdatedAt: now}, nil
}

func (s *Store) ResolveWorkspace(workspaceID string) (WorkspaceBinding, error) {
	if s == nil || s.db == nil || !workspaceIDPattern.MatchString(workspaceID) {
		return WorkspaceBinding{}, errors.New("workspace id is invalid")
	}
	var binding WorkspaceBinding
	var updatedAt int64
	err := s.db.QueryRow(`SELECT w.workspace_id,w.device_id,w.profile,w.mode,w.updated_at FROM edge_workspaces w JOIN devices d ON d.device_id=w.device_id WHERE w.workspace_id=? AND d.state=?`, workspaceID, StateActive).Scan(
		&binding.WorkspaceID, &binding.DeviceID, &binding.Profile, &binding.Mode, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceBinding{}, errors.New("registered active workspace not found")
	}
	if err != nil {
		return WorkspaceBinding{}, errors.New("workspace registry unavailable")
	}
	if !validWorkspaceRegistration(WorkspaceRegistration{WorkspaceID: binding.WorkspaceID, Profile: binding.Profile, Mode: binding.Mode}) {
		return WorkspaceBinding{}, errors.New("workspace registration is invalid")
	}
	binding.UpdatedAt = time.Unix(0, updatedAt).UTC()
	return binding, nil
}
