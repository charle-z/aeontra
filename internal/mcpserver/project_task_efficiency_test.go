package mcpserver

import (
	"context"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"testing"
	"time"
)

func TestProjectTaskEfficiencyIsOptionalAndDoesNotInvalidateHandoff(t *testing.T) {
	s, store := modelTurnServer(t)
	runtime, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	view := projectTaskView{TaskID: "tg_11111111111111111111111111111111", Workers: []projectTaskWorkerView{{Ordinal: 0, RuntimeID: runtime.RuntimeID}, {Ordinal: 1}}}
	before := projectTaskCheckpointRevision(view)
	s.attachProjectTaskEfficiency(context.Background(), &view)
	if view.Workers[0].Efficiency == nil || view.Workers[0].EfficiencyState != "observed" || view.Workers[1].Efficiency != nil || view.Workers[1].EfficiencyState != "unavailable" {
		t.Fatalf("metrics=%+v", view.Workers)
	}
	if projectTaskCheckpointRevision(view) != before {
		t.Fatal("advisory metrics invalidated handoff")
	}
	view.Workers[0].Efficiency.ElapsedMS = new(int64)
	*view.Workers[0].Efficiency.ElapsedMS = 100
	if projectTaskCheckpointRevision(view) != before {
		t.Fatal("elapsed time invalidated handoff")
	}
	view.Workers[0].Control = &modelturn.RuntimeControl{Generation: 1}
	if projectTaskCheckpointRevision(view) == before {
		t.Fatal("ownership not bound to handoff revision")
	}
	view.Workers[0].Control.Generation = 2
	finalizeProjectTaskView(&view, time.Now().UTC())
	if view.Handoff == nil {
		t.Fatal("handoff missing")
	}
}
