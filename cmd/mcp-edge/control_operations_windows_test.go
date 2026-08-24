//go:build windows

package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
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
		_, code := executeWindowsControlOperation(context.Background(), t.TempDir(), nil, edge.Operation{Kind: kind})
		if code == "" || code == "operation_invalid" {
			t.Fatalf("kind=%s was not rejected with a capability-specific safe code: %q", kind, code)
		}
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
