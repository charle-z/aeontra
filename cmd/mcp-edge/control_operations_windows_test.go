//go:build windows

package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type windowsProgressFake struct {
	controls []edge.OperationControl
	seen     []edge.OperationProgress
}

func (fake *windowsProgressFake) ReportOperationProgress(_ context.Context, _, _ string, progress edge.OperationProgress) (edge.OperationControl, error) {
	fake.seen = append(fake.seen, progress)
	if len(fake.controls) == 0 {
		return edge.OperationControl{}, nil
	}
	control := fake.controls[0]
	fake.controls = fake.controls[1:]
	return control, nil
}

func TestWindowsControlRejectsUnownedCapabilities(t *testing.T) {
	for _, kind := range []edge.OperationKind{
		edge.OperationProjectBrowserCreate,
		edge.OperationProjectBrowserHarnessStart,
		edge.OperationProjectToolboxCreate,
		edge.OperationAutopilotStart,
		edge.OperationBundleUpdate,
		edge.OperationEdgeRepair,
	} {
		_, code := executeWindowsControlOperation(context.Background(), t.TempDir(), nil, 0, edge.Operation{Kind: kind})
		if code == "" || code == "operation_invalid" {
			t.Fatalf("kind=%s was not rejected with a capability-specific safe code: %q", kind, code)
		}
	}
}

func TestWindowsControlRoutesReadOnlyDiagnosticsToNativeCollector(t *testing.T) {
	original := collectWindowsEdgeDiagnostic
	t.Cleanup(func() { collectWindowsEdgeDiagnostic = original })
	want := edge.OperationResult{
		Release:              "v1.2.25",
		Commit:               "0123456789abcdef0123456789abcdef01234567",
		ManifestStatus:       "valid",
		ComponentsCompatible: true,
		ProviderValid:        true,
		DriverValid:          true,
	}
	called := 0
	collectWindowsEdgeDiagnostic = func(workspaceCount int) (edge.OperationResult, string) {
		called++
		if workspaceCount != 2 {
			t.Fatalf("workspace count=%d", workspaceCount)
		}
		return want, ""
	}
	for _, kind := range []edge.OperationKind{edge.OperationBundleStatus, edge.OperationOnboardingStatus} {
		got, code := executeWindowsControlOperation(context.Background(), `D:\Aeontra\State`, nil, 2, edge.Operation{Kind: kind})
		if code != "" || !reflect.DeepEqual(got, want) {
			t.Fatalf("kind=%s result=%+v code=%q", kind, got, code)
		}
	}
	if called != 2 {
		t.Fatalf("diagnostic collector calls=%d want=2", called)
	}
}

func TestWindowsControlCancellationStopsExecutorBeforeReturn(t *testing.T) {
	fake := &windowsProgressFake{controls: []edge.OperationControl{{CancelRequested: true}}}
	lease := edge.OperationLease{Operation: edge.Operation{ID: "eo_0123456789abcdef0123456789abcdef"}, LeaseID: "el_0123456789abcdef0123456789abcdef"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, code, cancelled, err := executeWindowsControlOperationLifecycle(ctx, fake, lease, func(ctx context.Context) (edge.OperationResult, string) {
		t.Fatal("executor started despite cancellation request")
		return edge.OperationResult{}, "operation_failed"
	})
	if err != nil || !cancelled || code != "" || !reflect.DeepEqual(result, edge.OperationResult{}) {
		t.Fatalf("result=%+v code=%q cancelled=%v err=%v", result, code, cancelled, err)
	}
	if len(fake.seen) != 1 || fake.seen[0].Phase != "running" {
		t.Fatalf("progress=%+v", fake.seen)
	}
}

func TestWindowsProjectPreparationOwnsWorkspaceProfile(t *testing.T) {
	request := windowsProjectPreparationRequest(edge.OperationRequest{
		Alias:       "project",
		Repository:  "repo",
		TargetAlias: "windows-trusted",
		Profile:     "linux-workcell",
	})
	if request.Profile != edgeclient.WorkspaceProfileWindowsWorkcell {
		t.Fatalf("profile=%q want=%q", request.Profile, edgeclient.WorkspaceProfileWindowsWorkcell)
	}
	if request.Alias != "project" || request.Repository != "repo" || request.TargetAlias != "windows-trusted" {
		t.Fatalf("request fields changed: %+v", request)
	}
}

func TestWindowsControlProcessListPreservesTerminalMetadata(t *testing.T) {
	startedAt := time.Date(2026, time.August, 28, 1, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(5 * time.Minute)
	result := windowsProjectProcessListResult(edgeclient.ProjectResolution{}, []edgeclient.ProjectProcessSnapshot{{
		ProcessID:  "pr_0123456789abcdef0123456789abcdef",
		State:      edgeclient.ProjectProcessStopped,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Reason:     "process_stopped_while_offline",
	}})
	if len(result.BackgroundProcesses) != 1 {
		t.Fatalf("process count=%d want=1", len(result.BackgroundProcesses))
	}
	got := result.BackgroundProcesses[0]
	if got.StartedAt != startedAt.Format(time.RFC3339Nano) || got.FinishedAt != finishedAt.Format(time.RFC3339Nano) || got.Reason != "process_stopped_while_offline" {
		t.Fatalf("terminal metadata was not preserved: %+v", got)
	}
}
