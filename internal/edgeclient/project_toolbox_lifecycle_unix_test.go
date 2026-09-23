//go:build !windows

package edgeclient

import "testing"

func TestProjectToolboxReclaimabilityRequiresExplicitDisposableAndStoppedState(t *testing.T) {
	persistent := projectToolboxRecord{Lifecycle: projectToolboxPersistent}
	if reclaimable, reason := projectToolboxReclaimability(persistent, ProjectToolboxStopped); reclaimable || reason != "persistent" {
		t.Fatalf("persistent reclaimable=%t reason=%q", reclaimable, reason)
	}

	disposable := projectToolboxRecord{Lifecycle: projectToolboxDisposable}
	for _, test := range []struct {
		state       ProjectToolboxState
		reclaimable bool
		reason      string
	}{
		{state: ProjectToolboxRunning, reason: "active"},
		{state: ProjectToolboxCreated, reason: "active"},
		{state: ProjectToolboxUnknown, reason: "unknown_state"},
		{state: ProjectToolboxStopped, reclaimable: true, reason: "eligible"},
	} {
		reclaimable, reason := projectToolboxReclaimability(disposable, test.state)
		if reclaimable != test.reclaimable || reason != test.reason {
			t.Fatalf("state=%s reclaimable=%t reason=%q want=%t/%q", test.state, reclaimable, reason, test.reclaimable, test.reason)
		}
	}
}

func TestProjectToolboxReclaimabilityFailsClosedOnActiveChildren(t *testing.T) {
	record := projectToolboxRecord{
		Lifecycle: projectToolboxDisposable,
		Services:  []projectToolboxServiceRecord{{State: "running"}},
	}
	if reclaimable, reason := projectToolboxReclaimability(record, ProjectToolboxStopped); reclaimable || reason != "service_active" {
		t.Fatalf("service reclaimable=%t reason=%q", reclaimable, reason)
	}

	record.Services = []projectToolboxServiceRecord{{State: "stopped"}}
	record.BrowserHarnessRuns = []projectBrowserHarnessRecord{{State: "running"}}
	if reclaimable, reason := projectToolboxReclaimability(record, ProjectToolboxStopped); reclaimable || reason != "browser_active" {
		t.Fatalf("browser reclaimable=%t reason=%q", reclaimable, reason)
	}

	record.BrowserHarnessRuns[0].State = "stopped"
	if reclaimable, reason := projectToolboxReclaimability(record, ProjectToolboxStopped); !reclaimable || reason != "eligible" {
		t.Fatalf("terminal children reclaimable=%t reason=%q", reclaimable, reason)
	}
}
