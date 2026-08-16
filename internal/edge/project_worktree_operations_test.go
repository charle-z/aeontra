package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestProjectWorktreeOperationRequiresExactFencedIdentity(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	request := OperationRequest{
		Alias: "Project", TargetAlias: "Parrot", Profile: "linux-workcell",
		IdempotencyKey: "task-worktree-create-1", WorktreeBaseCommit: "0123456789abcdef0123456789abcdef01234567", WorktreeRole: "writer",
		WorkJobID: "wj_0123456789abcdef0123456789abcdef", WorkLeaseID: "wl_0123456789abcdef0123456789abcdef", WorkFence: 1,
	}
	operation, fresh, err := store.CreateOperation(device.ID, OperationProjectWorktreeCreate, request)
	if err != nil || !fresh || operation.Request.Alias != "project" || operation.Request.TargetAlias != "parrot" {
		t.Fatalf("operation=%+v fresh=%v err=%v", operation, fresh, err)
	}
	recovered, found, err := store.OperationByIdempotency(device.ID, OperationProjectWorktreeCreate, request.IdempotencyKey)
	if err != nil || !found || recovered.ID != operation.ID {
		t.Fatalf("recovered=%+v found=%v err=%v", recovered, found, err)
	}
	if _, found, err := store.OperationByIdempotency(device.ID, OperationProjectWorktreeCreate, "task-worktree-missing-1"); err != nil || found {
		t.Fatalf("missing lookup found=%v err=%v", found, err)
	}
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	result := OperationResult{
		WorkspaceID:  "ws_0123456789abcdef0123456789abcdef",
		ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectTarget: "parrot",
		ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		WorktreeID: "wt_0123456789abcdef0123456789abcdef", WorktreeState: "ready", WorktreeRole: "writer",
		WorktreeBaseCommit: request.WorktreeBaseCommit, WorktreeBranch: "codex/worktree-0123456789abcdef0123456789abcdef",
		WorkJobID: request.WorkJobID, WorkLeaseID: request.WorkLeaseID, WorkFence: 1,
		WorktreeCreatedAt: now.Format(time.RFC3339Nano), WorktreeUpdatedAt: now.Format(time.RFC3339Nano),
	}
	completed, err := store.CompleteOperation(device.ID, operation.ID, lease.LeaseID, result, "")
	if err != nil || completed.State != OperationSucceeded {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	for _, invalid := range []OperationRequest{
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "task-worktree-create-2", WorktreeBaseCommit: request.WorktreeBaseCommit, WorktreeRole: "writer", WorkJobID: request.WorkJobID, WorkLeaseID: request.WorkLeaseID},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", WorktreeID: result.WorktreeID, WorkJobID: request.WorkJobID, WorkLeaseID: request.WorkLeaseID, WorkFence: 1, Argv: []string{"pwd"}},
	} {
		kind := OperationProjectWorktreeCreate
		if invalid.WorktreeID != "" {
			kind = OperationProjectWorktreeClaim
		}
		if _, _, err := store.CreateOperation(device.ID, kind, invalid); err == nil {
			t.Fatalf("invalid worktree request accepted: %+v", invalid)
		}
	}
	unsafe := result
	unsafe.WorkFence = 0
	if validOperationCompletionForKind(OperationProjectWorktreeCreate, unsafe, "") {
		t.Fatal("unfenced worktree result accepted")
	}
}

func TestProjectWorktreeListResultIsBoundedAndTyped(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result := OperationResult{
		WorkspaceID:  "ws_0123456789abcdef0123456789abcdef",
		ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectTarget: "parrot",
		ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		Worktrees: []ProjectWorktreeSummary{{
			WorktreeID: "wt_0123456789abcdef0123456789abcdef", WorkspaceID: "ws_abcdefabcdefabcdefabcdefabcdefab",
			State: "ready", Role: "reader", BaseCommit: "0123456789abcdef0123456789abcdef01234567",
			Branch: "codex/worktree-0123456789abcdef0123456789abcdef", JobID: "wj_0123456789abcdef0123456789abcdef",
			LeaseID: "wl_0123456789abcdef0123456789abcdef", Fence: 1, CreatedAt: now, UpdatedAt: now,
		}},
	}
	if !validOperationCompletionForKind(OperationProjectWorktreeList, result, "") {
		t.Fatal("valid worktree list rejected")
	}
	result.Worktrees[0].Branch = "main"
	if validOperationCompletionForKind(OperationProjectWorktreeList, result, "") {
		t.Fatal("caller-controlled worktree branch accepted")
	}
}

func TestProjectWorktreeStatusRequiresBoundedGitEvidence(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result := OperationResult{
		WorkspaceID:  "ws_0123456789abcdef0123456789abcdef",
		ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectTarget: "parrot",
		ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		WorktreeID: "wt_0123456789abcdef0123456789abcdef", WorktreeState: "ready", WorktreeRole: "writer",
		WorktreeBaseCommit: "0123456789abcdef0123456789abcdef01234567", WorktreeBranch: "codex/worktree-0123456789abcdef0123456789abcdef",
		WorkJobID: "wj_0123456789abcdef0123456789abcdef", WorkLeaseID: "wl_0123456789abcdef0123456789abcdef", WorkFence: 1,
		WorktreeCreatedAt: now, WorktreeUpdatedAt: now,
		WorktreeEvidenceKnown: true, WorktreeHeadCommit: "1123456789abcdef0123456789abcdef01234567", WorktreeClean: true,
		WorktreeCommitsAheadBase: 1, WorktreeChangedPathCount: 2,
	}
	if !validOperationCompletionForKind(OperationProjectWorktreeStatus, result, "") {
		t.Fatal("valid worktree status evidence rejected")
	}
	withoutEvidence := result
	withoutEvidence.WorktreeEvidenceKnown = false
	if validOperationCompletionForKind(OperationProjectWorktreeStatus, withoutEvidence, "") {
		t.Fatal("worktree status without Git evidence accepted")
	}
	leaky := result
	leaky.ExecStdout = "secret path"
	if validOperationCompletionForKind(OperationProjectWorktreeStatus, leaky, "") {
		t.Fatal("mixed worktree status result accepted")
	}
}
