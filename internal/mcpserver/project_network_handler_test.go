package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectNetworkToolStore struct {
	projectExecToolStore
	fail bool
}

func (store *projectNetworkToolStore) WaitOperation(_ context.Context, operationID string, _ time.Duration) (edge.Operation, error) {
	result := edge.OperationResult{
		WorkspaceID: "ws_33333333333333333333333333333333", ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo",
		ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		NetworkDestination: "10.64.140.182", NetworkInterface: "tun0", NetworkSourceIP: "192.168.165.249",
	}
	if store.createdKind == edge.OperationProjectNetworkProbe {
		result.NetworkPorts = []edge.NetworkPortResult{{Port: 21, State: "open"}, {Port: 22, State: "closed"}}
	}
	state, safeCode := edge.OperationSucceeded, ""
	if store.fail {
		state, safeCode, result = edge.OperationFailed, "project_network_route_failed", edge.OperationResult{}
	}
	return edge.Operation{ID: operationID, Kind: store.createdKind, State: state, SafeCode: safeCode, Result: result}, nil
}

func TestProjectNetworkToolsQueueStructuredOperationsAndReturnSafeResults(t *testing.T) {
	store := &projectNetworkToolStore{}
	server := New(nil).WithEdgeStore(store)

	route, err := server.table["project_network_route"].handler(json.RawMessage(`{"alias":"project","target":"parrot","destination":"10.64.140.182"}`))
	if err != nil {
		t.Fatal(err)
	}
	if store.resolvedTarget != "parrot" || store.createdKind != edge.OperationProjectNetworkRoute {
		t.Fatalf("target=%q kind=%q", store.resolvedTarget, store.createdKind)
	}
	wantRoute := edge.OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", NetworkDestination: "10.64.140.182"}
	if !reflect.DeepEqual(store.createdRequest, wantRoute) {
		t.Fatalf("route request=%+v", store.createdRequest)
	}
	for _, required := range []string{`"state":"succeeded"`, `"repository":"charle-z/repo"`, `"destination":"10.64.140.182"`, `"interface":"tun0"`, `"source_ip":"192.168.165.249"`} {
		if !strings.Contains(route, required) {
			t.Fatalf("route output missing %q: %s", required, route)
		}
	}
	for _, forbidden := range []string{"device_id", "workspace_id", "argv", "environment"} {
		if strings.Contains(route, forbidden) {
			t.Fatalf("route output exposed %q: %s", forbidden, route)
		}
	}

	probe, err := server.table["project_network_probe"].handler(json.RawMessage(`{"alias":"project","target":"parrot","destination":"10.64.140.182","ports":[21,22],"timeout_ms":500}`))
	if err != nil {
		t.Fatal(err)
	}
	wantProbe := wantRoute
	wantProbe.NetworkPorts = []int{21, 22}
	wantProbe.NetworkTimeoutMillis = 500
	if store.createdKind != edge.OperationProjectNetworkProbe || !reflect.DeepEqual(store.createdRequest, wantProbe) {
		t.Fatalf("probe kind=%q request=%+v", store.createdKind, store.createdRequest)
	}
	if !strings.Contains(probe, `"ports":[{"port":21,"state":"open"},{"port":22,"state":"closed"}]`) {
		t.Fatalf("probe output=%s", probe)
	}
}

func TestProjectNetworkHandlerFailsClosed(t *testing.T) {
	server := New(nil)
	if _, err := server.handleProjectNetwork(json.RawMessage(`{}`), edge.OperationProjectNetworkRoute); !errors.Is(err, errEdgeStoreUnavailable) {
		t.Fatalf("missing store error=%v", err)
	}

	store := &projectNetworkToolStore{fail: true}
	server.WithEdgeStore(store)
	output, err := server.handleProjectNetwork(json.RawMessage(`{"alias":"project","target":"parrot","destination":"10.64.140.182"}`), edge.OperationProjectNetworkRoute)
	if err != nil || !strings.Contains(output, `"state":"failed"`) || !strings.Contains(output, `"reason":"project_network_route_failed"`) {
		t.Fatalf("failure output=%s err=%v", output, err)
	}
}
