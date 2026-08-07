package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectOperationParams struct {
	Alias      string `json:"alias"`
	Repository string `json:"repository,omitempty"`
	Target     string `json:"target"`
}

type projectOperationPublicView struct {
	Alias      string `json:"alias"`
	Repository string `json:"repository,omitempty"`
	Target     string `json:"target"`
	State      string `json:"state"`
	Profile    string `json:"profile,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type edgeDeviceAliasRegistry interface {
	ResolveActiveDeviceName(string) (edge.Device, error)
}

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
	DeviceID              string              `json:"device_id,omitempty"`
	State                 edge.OperationState `json:"state"`
	WorkspaceID           string              `json:"workspace_id,omitempty"`
	AuthorizationRevision uint64              `json:"authorization_revision,omitempty"`
	SafeCode              string              `json:"safe_code,omitempty"`
	JobID                 string              `json:"job_id,omitempty"`
	JobState              string              `json:"job_state,omitempty"`
	ProgressRevision      uint64              `json:"progress_revision,omitempty"`
	CycleCount            uint64              `json:"cycle_count,omitempty"`
	JobSafeCode           string              `json:"job_safe_code,omitempty"`
	Release               string              `json:"release,omitempty"`
	Commit                string              `json:"commit,omitempty"`
	ManifestStatus        string              `json:"manifest_status,omitempty"`
	ComponentsCompatible  bool                `json:"components_compatible,omitempty"`
	ServiceActive         bool                `json:"service_active,omitempty"`
	ServiceState          string              `json:"service_state,omitempty"`
	ServiceRestarts       uint64              `json:"service_restarts,omitempty"`
	ServiceRestartsKnown  bool                `json:"service_restarts_known,omitempty"`
	ProcessState          string              `json:"process_state,omitempty"`
	LockState             string              `json:"lock_state,omitempty"`
	Coherence             string              `json:"coherence,omitempty"`
	ProcessRelease        string              `json:"process_release,omitempty"`
	ProcessCommit         string              `json:"process_commit,omitempty"`
	UpdateAvailable       bool                `json:"update_available"`
	Paired                bool                `json:"paired,omitempty"`
	BubblewrapValid       bool                `json:"bubblewrap_valid,omitempty"`
	RootlessValid         bool                `json:"rootless_valid,omitempty"`
	WorkspaceCount        int                 `json:"workspace_count,omitempty"`
	ProviderValid         bool                `json:"provider_valid,omitempty"`
	DriverValid           bool                `json:"driver_valid,omitempty"`
	Blockers              []string            `json:"blockers,omitempty"`
}

func (s *Server) addEdgeControlTools() {
	writeHints := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	projectPrepareHints := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}
	projectSchema := map[string]any{
		"alias":      stringSchema("human project alias", `^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`, 63),
		"repository": stringSchema("repository name under the Edge local GitHub owner", `^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`, 100),
		"target":     stringSchema("human paired Edge alias", `^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`, 32),
	}
	s.addDirectTool(toolDef{
		Name: "project_prepare", Description: "Create, recover, or associate one development project on a paired Edge using only alias, repository name, and human target alias. The Edge revalidates local Git authority and checkout state; paths and opaque IDs are not accepted or returned.",
		InputSchema: closedObject(projectSchema, []string{"alias", "repository", "target"}), Version: "1", Annotations: projectPrepareHints,
	}, func(arguments json.RawMessage) (string, error) {
		return s.handleProjectOperation(arguments, edge.OperationProjectPrepare)
	})
	s.addDirectTool(toolDef{
		Name: "project_status", Description: "Resolve one registered Edge project by human alias and target alias, returning only safe repository and readiness metadata.",
		InputSchema: closedObject(map[string]any{"alias": projectSchema["alias"], "target": projectSchema["target"]}, []string{"alias", "target"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
	}, func(arguments json.RawMessage) (string, error) {
		return s.handleProjectOperation(arguments, edge.OperationProjectStatus)
	})
	s.addDirectTool(toolDef{
		Name: "project_snapshot", Description: "Run one fixed read-only Git snapshot in the selected Edge development workspace through a durable idempotent operation. It returns only bounded repository identity, branch, commit and clean-state metadata; it does not start a model.",
		InputSchema: closedObject(map[string]any{
			"alias": projectSchema["alias"], "target": projectSchema["target"],
			"idempotency_key": stringSchema("caller-generated durable operation key", `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`, 128),
		}, []string{"alias", "target", "idempotency_key"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
	}, s.handleProjectSnapshot)
	s.addProjectExecTool(projectSchema)
	s.addProjectNetworkTools(projectSchema)
	s.addProjectProcessTools(projectSchema)
	s.addProjectGitSyncTools(projectSchema)
	s.addProjectGitHubTools(projectSchema)
	s.addProjectToolboxTools(projectSchema)
	s.addProjectBrowserHarnessTools(projectSchema)
	s.addProjectBrowserTools(projectSchema)
	s.addEdgeOperationLifecycleTools(projectSchema)
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
	deviceSchema := map[string]any{"device_id": stringSchema("opaque active Edge device id", `^ed_[a-f0-9]{32}$`, 35)}
	s.addDirectTool(toolDef{Name: "edge_bundle_status", Description: "Return signed-bundle and service compatibility metadata from one paired Edge.", InputSchema: closedObject(deviceSchema, []string{"device_id"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}, func(arguments json.RawMessage) (string, error) {
		return s.handleDeviceOperation(arguments, edge.OperationBundleStatus, false)
	})
	s.addDirectTool(toolDef{Name: "edge_bundle_update", Description: "Request only the official signed stable bundle through the restricted privileged updater.", InputSchema: closedObject(map[string]any{"device_id": deviceSchema["device_id"], "release": map[string]any{"type": "string", "enum": []string{"stable"}}}, []string{"device_id", "release"}), Version: "1", Annotations: writeHints}, func(arguments json.RawMessage) (string, error) {
		return s.handleDeviceOperation(arguments, edge.OperationBundleUpdate, true)
	})
	s.addDirectTool(toolDef{Name: "edge_bundle_rollback", Description: "Roll back one paired Edge only to its previous known signed bundle.", InputSchema: closedObject(deviceSchema, []string{"device_id"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false}}, func(arguments json.RawMessage) (string, error) {
		return s.handleDeviceOperation(arguments, edge.OperationBundleRollback, false)
	})
	s.addDirectTool(toolDef{Name: "edge_repair", Description: "Restore only official signed MCP Devbox components, links, permissions, unit and Edge health.", InputSchema: closedObject(deviceSchema, []string{"device_id"}), Version: "1", Annotations: writeHints}, func(arguments json.RawMessage) (string, error) {
		return s.handleDeviceOperation(arguments, edge.OperationEdgeRepair, false)
	})
	s.addDirectTool(toolDef{Name: "edge_onboarding_status", Description: "Return safe paired, service, bundle, provider, driver, Bubblewrap, rootless, workspace-count and blocker metadata.", InputSchema: closedObject(deviceSchema, []string{"device_id"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}, func(arguments json.RawMessage) (string, error) {
		return s.handleDeviceOperation(arguments, edge.OperationOnboardingStatus, false)
	})
}

func (s *Server) handleProjectOperation(arguments json.RawMessage, kind edge.OperationKind) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	var params projectOperationParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	if kind == edge.OperationProjectStatus && params.Repository != "" {
		return "", errors.New("repository is not accepted for project status")
	}
	device, err := resolver.ResolveActiveDeviceName(params.Target)
	if err != nil {
		return "", err
	}
	request := edge.OperationRequest{Alias: params.Alias, Repository: params.Repository, TargetAlias: params.Target, Profile: "linux-workcell"}
	op, _, err := s.edgeOperations.CreateOperation(device.ID, kind, request)
	if err == nil {
		op, err = s.edgeOperations.WaitOperation(context.Background(), op.ID, 180*time.Second)
	}
	view := projectOperationPublicView{Alias: params.Alias, Target: params.Target, State: string(op.State)}
	if op.State == edge.OperationSucceeded {
		view.Alias = op.Result.ProjectAlias
		view.Repository = op.Result.ProjectOwner + "/" + op.Result.ProjectRepository
		view.Target = op.Result.ProjectTarget
		view.State = op.Result.ProjectState
		view.Profile = op.Result.ProjectProfile
		view.Mode = op.Result.ProjectMode
	} else if op.State == edge.OperationFailed {
		view.Reason = op.SafeCode
	}
	return marshalToolValue(view, err)
}

func (s *Server) handleProjectSnapshot(arguments json.RawMessage) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	var params projectSnapshotParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	device, err := resolver.ResolveActiveDeviceName(params.Target)
	if err != nil {
		return "", err
	}
	request := edge.OperationRequest{
		Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell",
		IdempotencyKey: params.IdempotencyKey,
	}
	operation, created, err := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectSnapshot, request)
	if err == nil {
		operation, err = s.edgeOperations.WaitOperation(context.Background(), operation.ID, 180*time.Second)
	}
	reused := err == nil && !created
	view := projectSnapshotPublicView{
		OperationID: operation.ID, State: operation.State, Alias: params.Alias,
		Target: params.Target, Reused: reused,
	}
	if operation.State == edge.OperationSucceeded {
		view.Alias = operation.Result.ProjectAlias
		view.Repository = operation.Result.ProjectOwner + "/" + operation.Result.ProjectRepository
		view.Target = operation.Result.ProjectTarget
		view.Profile = operation.Result.ProjectProfile
		view.Mode = operation.Result.ProjectMode
		view.Branch = operation.Result.SnapshotBranch
		view.Head = operation.Result.SnapshotHead
		view.Clean = operation.Result.SnapshotClean
	} else if operation.State == edge.OperationFailed {
		view.Reason = operation.SafeCode
	}
	return marshalToolValue(view, err)
}

type deviceOperationParams struct {
	DeviceID string `json:"device_id"`
	Release  string `json:"release,omitempty"`
}

func (s *Server) handleDeviceOperation(arguments json.RawMessage, kind edge.OperationKind, requiresStable bool) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	var params deviceOperationParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	if !s.edgeDevices.DeviceActive(params.DeviceID) {
		return "", errors.New("active edge device not found")
	}
	request := edge.OperationRequest{}
	if requiresStable {
		request.Release = params.Release
	} else if params.Release != "" {
		return "", errors.New("release is not accepted")
	}
	operation, _, err := s.edgeOperations.CreateOperation(params.DeviceID, kind, request)
	if err == nil {
		operation, err = s.edgeOperations.WaitOperation(context.Background(), operation.ID, 180*time.Second)
	}
	return marshalToolValue(publicEdgeOperation(operation), err)
}

type autopilotControlParams struct {
	WorkspaceID string `json:"workspace_id"`
	RunUntil    string `json:"run_until,omitempty"`
}

type workspaceIDParams struct {
	WorkspaceID string `json:"workspace_id"`
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
	var params workspaceIDParams
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
	view := edgeOperationPublicView{OperationID: op.ID, DeviceID: op.DeviceID, State: op.State, WorkspaceID: op.Result.WorkspaceID, AuthorizationRevision: op.Result.AuthorizationRevision, SafeCode: op.SafeCode, JobID: op.Result.JobID, JobState: op.Result.JobState, ProgressRevision: op.Result.ProgressRevision, CycleCount: op.Result.CycleCount, JobSafeCode: op.Result.JobSafeCode, Release: op.Result.Release, Commit: op.Result.Commit, ManifestStatus: op.Result.ManifestStatus, ComponentsCompatible: op.Result.ComponentsCompatible, ServiceActive: op.Result.ServiceActive, UpdateAvailable: op.Result.UpdateAvailable, Paired: op.Result.Paired, BubblewrapValid: op.Result.BubblewrapValid, RootlessValid: op.Result.RootlessValid, WorkspaceCount: op.Result.WorkspaceCount, ProviderValid: op.Result.ProviderValid, DriverValid: op.Result.DriverValid, Blockers: op.Result.Blockers}
	view.ServiceState = op.Result.ServiceState
	view.ServiceRestarts = op.Result.ServiceRestarts
	view.ServiceRestartsKnown = op.Result.ServiceRestartsKnown
	view.ProcessState = op.Result.ProcessState
	view.LockState = op.Result.LockState
	view.Coherence = op.Result.Coherence
	view.ProcessRelease = op.Result.ProcessRelease
	view.ProcessCommit = op.Result.ProcessCommit
	return view
}

type projectSnapshotParams struct {
	Alias          string `json:"alias"`
	Target         string `json:"target"`
	IdempotencyKey string `json:"idempotency_key"`
}

type projectSnapshotPublicView struct {
	OperationID string              `json:"operation_id"`
	State       edge.OperationState `json:"state"`
	Alias       string              `json:"alias"`
	Repository  string              `json:"repository,omitempty"`
	Target      string              `json:"target"`
	Profile     string              `json:"profile,omitempty"`
	Mode        string              `json:"mode,omitempty"`
	Branch      string              `json:"branch,omitempty"`
	Head        string              `json:"head,omitempty"`
	Clean       bool                `json:"clean"`
	Reused      bool                `json:"reused"`
	Reason      string              `json:"reason,omitempty"`
}
