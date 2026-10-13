package edge

import (
	"strings"
	"testing"
)

func TestWorktreeStatusAncestryInputsAreBoundedExactAndReadOnly(t *testing.T) {
	r := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", WorktreeID: "wt_11111111111111111111111111111111", WorktreeAncestorCommits: []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}}
	if _, err := normalizeProjectWorktreeRequest(OperationProjectWorktreeStatus, r); err != nil {
		t.Fatal(err)
	}
	for _, heads := range [][]string{{"main"}, {"--force"}, {strings.Repeat("a", 40), strings.Repeat("a", 40)}, make([]string, 5)} {
		bad := r
		bad.WorktreeAncestorCommits = heads
		if _, err := normalizeProjectWorktreeRequest(OperationProjectWorktreeStatus, bad); err == nil {
			t.Fatalf("invalid probes accepted: %v", heads)
		}
	}
	for _, kind := range []OperationKind{OperationProjectWorktreeCreate, OperationProjectWorktreeClaim, OperationProjectWorktreeCleanup, OperationProjectWorktreeList} {
		if _, err := normalizeProjectWorktreeRequest(kind, r); err == nil {
			t.Fatalf("ancestry expanded effect %s", kind)
		}
	}
	if emptyProjectWorktreeRequestFields(r) {
		t.Fatal("ancestry inputs ignored by unrelated-operation guard")
	}
}
