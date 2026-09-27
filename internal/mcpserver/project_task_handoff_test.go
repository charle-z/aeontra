package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

func TestProjectTaskHandoffIsBoundedAndInvalidatesOnProgress(t *testing.T) {
	now := time.Date(2026, 9, 27, 21, 0, 0, 0, time.UTC)
	older := now.Add(-2 * time.Minute)
	newer := now.Add(-time.Minute)
	view := projectTaskView{
		TaskID: "tg_0123456789abcdef0123456789abcdef", State: "running", BaseCommit: strings.Repeat("a", 40),
		Workers: []projectTaskWorkerView{
			{Ordinal: 0, State: "running", LifecycleState: workqueue.StateLeased, RuntimeID: "mr_0123456789abcdef0123456789abcdef", RuntimeState: string(modelturn.RuntimeStateAwaitingModel), ActiveTurnCreatedAt: &newer, TurnSequence: 2, Summary: "private prompt must not enter handoff"},
			{Ordinal: 1, State: "running", LifecycleState: workqueue.StateLeased, RuntimeID: "mr_abcdef0123456789abcdef0123456789", RuntimeState: string(modelturn.RuntimeStateAwaitingModel), ActiveTurnCreatedAt: &older, TurnSequence: 1},
		},
	}
	finalizeProjectTaskView(&view, now)
	if view.Handoff == nil || view.Handoff.Version != 1 || !strings.HasPrefix(view.Handoff.Revision, "sha256:") {
		t.Fatalf("missing versioned handoff: %+v", view.Handoff)
	}
	if strings.Contains(view.Handoff.ResumePrompt, "private prompt") || !strings.Contains(view.Handoff.ResumePrompt, view.TaskID) || !strings.Contains(view.Handoff.ResumePrompt, "project_task_status") {
		t.Fatalf("handoff prompt is not bounded to durable identity: %+v", view.Handoff)
	}
	if len(view.AttentionOrder) != 2 || view.AttentionOrder[0] != 1 || view.AttentionOrder[1] != 0 {
		t.Fatalf("oldest pending turn must be first: %+v", view.AttentionOrder)
	}
	if view.Workers[0].ModelWaitSeconds == nil || *view.Workers[0].ModelWaitSeconds != 60 || view.Workers[1].ModelWaitSeconds == nil || *view.Workers[1].ModelWaitSeconds != 120 {
		t.Fatalf("pending durations are wrong: %+v", view.Workers)
	}
	firstRevision := view.Handoff.Revision
	finalizeProjectTaskView(&view, now.Add(10*time.Second))
	if view.Handoff.Revision != firstRevision {
		t.Fatal("passage of time alone must not invalidate a handoff")
	}
	view.Workers[1].TurnSequence++
	finalizeProjectTaskView(&view, now.Add(10*time.Second))
	if view.Handoff.Revision == firstRevision {
		t.Fatal("a new model turn must invalidate the old checkpoint")
	}
}

func TestProjectTaskHandoffDoesNotInventUnknownModelWait(t *testing.T) {
	view := projectTaskView{TaskID: "tg_0123456789abcdef0123456789abcdef", State: "running", Workers: []projectTaskWorkerView{{Ordinal: 0, State: "running", RuntimeState: string(modelturn.RuntimeStateAwaitingModel)}}}
	finalizeProjectTaskView(&view, time.Date(2026, 9, 27, 21, 0, 0, 0, time.UTC))
	if view.Workers[0].ModelWaitSeconds != nil || len(view.AttentionOrder) != 1 || view.AttentionOrder[0] != 0 {
		t.Fatalf("unknown timestamp must stay unknown while the worker remains actionable: %+v", view)
	}
}
