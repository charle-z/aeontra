package development

import (
	"strings"
	"testing"
)

func TestWorkspaceAnchorCannotBeRetargetedAfterCreation(t *testing.T) {
	scope, err := NewObjectiveScope("project", "parrot")
	if err != nil {
		t.Fatal(err)
	}
	scope.Anchor = WorkspaceAnchor{DeviceID: "ed_" + strings.Repeat("a", 32), WorkspaceID: "ws_" + strings.Repeat("b", 32), Generation: 1, Owner: "charle-z", Repository: "repo"}
	if !scope.Pinned() {
		t.Fatal("valid workspace anchor rejected")
	}
	policy, _ := NewResolutionPolicy(TierWorkcell, ClassWorkcell)
	objective, err := NewScopedObjective("anchored", scope, policy, []StepSpec{{StepID: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := objective.MarshalRecord()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ParseObjectiveRecord(body)
	if err != nil || restored.Scope != scope {
		t.Fatalf("scope roundtrip failed: %v", err)
	}
	for _, mutate := range []func(*WorkspaceAnchor){
		func(a *WorkspaceAnchor) { a.DeviceID = "ed_" + strings.Repeat("c", 32) },
		func(a *WorkspaceAnchor) { a.WorkspaceID = "ws_" + strings.Repeat("c", 32) },
		func(a *WorkspaceAnchor) { a.Generation++ },
		func(a *WorkspaceAnchor) { a.Repository = "other" },
	} {
		next := restored.clone()
		next.Revision++
		mutate(&next.Scope.Anchor)
		if ValidateTransition(restored, next) == nil {
			t.Fatal("retargeted anchor accepted")
		}
	}
}
