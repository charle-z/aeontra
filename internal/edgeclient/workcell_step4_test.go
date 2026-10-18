package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestStep4WorkcellDoesNotStartAnotherStageAfterCancellation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces")
	workspace := filepath.Join(root, "project")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/project\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := &step4CancellingCommandRunner{cancel: cancel}
	workcell := Workcell{Root: root, Commands: runner}
	task := edge.Task{
		Objective:    edge.Objective{Kind: edge.ObjectiveValidate, Summary: "validate"},
		Restrictions: edge.Restrictions{Workspace: "project", NetworkPolicy: edge.NetworkNone, MaxDurationSeconds: 60, MaxOutputBytes: 1024},
	}
	result := workcell.Execute(ctx, task)
	if runner.calls != 1 || result.Outcome != edge.OutcomeCancelled {
		t.Fatalf("calls=%d result=%+v", runner.calls, result)
	}
}

type step4CancellingCommandRunner struct {
	cancel context.CancelFunc
	calls  int
}

func (r *step4CancellingCommandRunner) Run(context.Context, string, []string, int64) (int, []byte, error) {
	r.calls++
	if r.calls == 1 {
		r.cancel()
	}
	return 0, nil, nil
}
