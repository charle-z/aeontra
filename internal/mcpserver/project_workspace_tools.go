package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectWorkspaceParams struct {
	Alias          string `json:"alias"`
	Target         string `json:"target"`
	WorktreeID     string `json:"worktree_id,omitempty"`
	BaseCommit     string `json:"base_commit,omitempty"`
	ExpectedHead   string `json:"expected_head,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

func (s *Server) addProjectWorkspaceTools(projectSchema map[string]any) {
	id := stringSchema("opaque direct managed workspace", `^wt_[a-f0-9]{32}$`, 35)
	commit := stringSchema("exact Git commit", `^[a-f0-9]{40}$`, 40)
	key := stringSchema("caller key; reuse it to recover the same workspace", `^[A-Za-z0-9][A-Za-z0-9._:-]{7,117}$`, 118)
	read := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	write := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	s.addDirectTool(toolDef{Name: "project_workspace_prepare", Description: "Create or recover an isolated exact-base writer worktree for direct MCP commands, without starting a model. Different keys create independent workspaces; reuse the same key and base after interruption. Source, credentials and unrelated workspaces are preserved.", Version: "1", Annotations: write,
		InputSchema: closedObject(map[string]any{"alias": projectSchema["alias"], "target": projectSchema["target"], "base_commit": commit, "idempotency_key": key}, []string{"alias", "target", "base_commit", "idempotency_key"})},
		func(args json.RawMessage) (string, error) {
			return s.handleProjectWorkspace(args, edge.OperationProjectWorktreeCreate)
		})
	s.addDirectTool(toolDef{Name: "project_workspace_status", Description: "List recent direct managed workspaces, or inspect one by worktree_id with bounded storage accounting. Exact status previews cleanup: HEAD, cleanliness and retained resources, without creating roots or changing source. Model worker worktrees cannot be selected.", Version: "1", Annotations: read,
		InputSchema: closedObject(map[string]any{"alias": projectSchema["alias"], "target": projectSchema["target"], "worktree_id": id, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, []string{"alias", "target"})},
		func(args json.RawMessage) (string, error) {
			return s.handleProjectWorkspace(args, edge.OperationProjectWorktreeStatus)
		})
	s.addDirectTool(toolDef{Name: "project_workspace_cleanup", Description: "Explicitly close the exact direct workspace previewed by status, bound to expected_head. Dirty/ignored source, detached heads and active resources are retained. Only its clean worktree and unreferenced private runtime/cache are reclaimed; commits on verified branches, populated artifacts and durable receipts remain. Never inferred from chat termination.", Version: "1",
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false},
		InputSchema: closedObject(map[string]any{"alias": projectSchema["alias"], "target": projectSchema["target"], "worktree_id": id, "expected_head": commit, "idempotency_key": key}, []string{"alias", "target", "worktree_id", "expected_head", "idempotency_key"})},
		func(args json.RawMessage) (string, error) {
			return s.handleProjectWorkspace(args, edge.OperationProjectWorktreeCleanup)
		})
}

func (s *Server) addDirectWorktreeSelectors() {
	for _, name := range []string{"project_process_start", "project_process_list", "project_process_cleanup", "project_toolbox_create", "project_toolbox_status", "project_toolbox_exec", "project_toolbox_install", "project_toolbox_repair", "project_toolbox_cleanup", "project_toolbox_service_start", "project_toolbox_service_status", "project_toolbox_service_stop", "project_git_status", "project_git_fetch", "project_git_publish_preview", "project_git_publish"} {
		entry := s.table[name]
		properties := entry.def.InputSchema["properties"].(map[string]any)
		properties["worktree_id"] = stringSchema("optional direct managed workspace; omit for the canonical project", `^wt_[a-f0-9]{32}$`, 35)
	}
}

func (s *Server) handleProjectWorkspace(args json.RawMessage, kind edge.OperationKind) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	var p projectWorkspaceParams
	if err := decodeClosed(args, &p); err != nil {
		return "", err
	}
	p.Alias, p.Target = strings.ToLower(strings.TrimSpace(p.Alias)), strings.ToLower(strings.TrimSpace(p.Target))
	if kind == edge.OperationProjectWorktreeCreate && (p.WorktreeID != "" || p.ExpectedHead != "" || p.Limit != 0) {
		return "", errors.New("workspace prepare contains unrelated fields")
	}
	if kind == edge.OperationProjectWorktreeStatus && (p.BaseCommit != "" || p.ExpectedHead != "" || p.IdempotencyKey != "" || p.Limit < 0 || p.Limit > 20 || (p.WorktreeID != "" && p.Limit != 0)) {
		return "", errors.New("workspace status contains unrelated fields")
	}
	if kind == edge.OperationProjectWorktreeCleanup && (p.BaseCommit != "" || p.Limit != 0 || p.WorktreeID == "") {
		return "", errors.New("workspace cleanup contains unrelated fields")
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	device, err := resolver.ResolveActiveDeviceName(p.Target)
	if err != nil {
		return "", err
	}
	r := edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", WorktreeRole: "direct", WorktreeID: p.WorktreeID, IdempotencyKey: p.IdempotencyKey}
	if kind == edge.OperationProjectWorktreeStatus && p.WorktreeID == "" {
		kind = edge.OperationProjectWorktreeList
		r.WorktreeLimit = p.Limit
		if r.WorktreeLimit == 0 {
			r.WorktreeLimit = 20
		}
	}
	if kind == edge.OperationProjectWorktreeCreate {
		sum := sha256.Sum256([]byte(p.Alias + "\x00" + p.Target + "\x00" + p.IdempotencyKey))
		digest := hex.EncodeToString(sum[:])
		r.WorktreeBaseCommit, r.WorkJobID, r.WorkLeaseID, r.WorkFence = p.BaseCommit, "wj_"+digest[:32], "wl_"+digest[32:], 1
	} else if kind == edge.OperationProjectWorktreeCleanup {
		// Read the captured binding; never accept caller job/lease/fence authority.
		status, _, err := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeStatus, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", WorktreeRole: "direct", WorktreeID: p.WorktreeID})
		if err == nil {
			status, err = s.edgeOperations.WaitOperation(context.Background(), status.ID, 30*time.Second)
		}
		if err != nil || status.State != edge.OperationSucceeded {
			return "", errors.New("direct workspace status unavailable; reconcile before cleanup")
		}
		r.WorktreeBaseCommit, r.WorkJobID, r.WorkLeaseID, r.WorkFence = p.ExpectedHead, status.Result.WorkJobID, status.Result.WorkLeaseID, status.Result.WorkFence
	}
	op, created, err := s.edgeOperations.CreateOperation(device.ID, kind, r)
	if err == nil {
		op, err = s.edgeOperations.WaitOperation(context.Background(), op.ID, 180*time.Second)
	}
	if kind == edge.OperationProjectWorktreeList {
		type item struct {
			WorktreeID string `json:"worktree_id"`
			State      string `json:"state"`
			Base       string `json:"base_commit"`
			CreatedAt  string `json:"created_at"`
		}
		items := make([]item, 0, len(op.Result.Worktrees))
		for _, w := range op.Result.Worktrees {
			items = append(items, item{w.WorktreeID, w.State, w.BaseCommit, w.CreatedAt})
		}
		return marshalToolValue(struct {
			OperationID string              `json:"operation_id"`
			State       edge.OperationState `json:"state"`
			Workspaces  []item              `json:"workspaces"`
			Reason      string              `json:"reason,omitempty"`
		}{op.ID, op.State, items, op.SafeCode}, err)
	}
	if kind == edge.OperationProjectWorktreeCreate && !created && err == nil && op.State == edge.OperationSucceeded {
		current, _, readErr := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeStatus, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", WorktreeRole: "direct", WorktreeID: op.Result.WorktreeID})
		if readErr == nil {
			current, readErr = s.edgeOperations.WaitOperation(context.Background(), current.ID, 30*time.Second)
		}
		if readErr != nil {
			return "", errors.New("workspace exists but current state is unavailable; reconcile by worktree id")
		}
		op = current
	}
	view := struct {
		OperationID  string                        `json:"operation_id"`
		State        edge.OperationState           `json:"state"`
		Alias        string                        `json:"alias"`
		Target       string                        `json:"target"`
		WorktreeID   string                        `json:"worktree_id,omitempty"`
		BaseCommit   string                        `json:"base_commit,omitempty"`
		Head         string                        `json:"head_commit,omitempty"`
		Branch       string                        `json:"managed_branch,omitempty"`
		Clean        bool                          `json:"clean"`
		CommitsAhead int                           `json:"commits_ahead_base"`
		Removed      bool                          `json:"removed"`
		Reused       bool                          `json:"reused"`
		Storage      *edge.ProjectWorkspaceStorage `json:"storage,omitempty"`
		Reason       string                        `json:"reason,omitempty"`
	}{op.ID, op.State, p.Alias, p.Target, op.Result.WorktreeID, op.Result.WorktreeBaseCommit, op.Result.WorktreeHeadCommit, op.Result.WorktreeBranch, op.Result.WorktreeClean, op.Result.WorktreeCommitsAheadBase, op.Result.WorktreeState == "removed", !created, op.Result.WorkspaceStorage, op.SafeCode}
	return marshalToolValue(view, err)
}
