package edge

import "testing"

func projectNetworkRequestFixture() OperationRequest {
	return OperationRequest{
		Alias: "mcp-devbox", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell",
		NetworkDestination: "10.64.140.182",
	}
}

func projectNetworkResultFixture() OperationResult {
	return OperationResult{
		WorkspaceID:  "ws_0123456789abcdef0123456789abcdef",
		ProjectAlias: "mcp-devbox", ProjectOwner: "charle-z", ProjectRepository: "mcp-devbox",
		ProjectTarget: "parrot-trusted-linux", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		NetworkDestination: "10.64.140.182", NetworkInterface: "tun0", NetworkSourceIP: "192.168.165.249",
	}
}

func TestNormalizeProjectNetworkRouteAcceptsOnlyPrivateStructuredDestination(t *testing.T) {
	request := projectNetworkRequestFixture()
	normalized, err := normalizeProjectNetworkRequest(OperationProjectNetworkRoute, request)
	if err != nil || normalized.NetworkDestination != "10.64.140.182" {
		t.Fatalf("normalized=%+v err=%v", normalized, err)
	}
	for _, destination := range []string{"8.8.8.8", "10.64.140.0/24", "example.com", ""} {
		invalid := request
		invalid.NetworkDestination = destination
		if _, err := normalizeProjectNetworkRequest(OperationProjectNetworkRoute, invalid); err == nil {
			t.Fatalf("accepted destination %q", destination)
		}
	}
	withPorts := request
	withPorts.NetworkPorts = []int{22}
	if _, err := normalizeProjectNetworkRequest(OperationProjectNetworkRoute, withPorts); err == nil {
		t.Fatal("route accepted ports")
	}
	withArgv := request
	withArgv.Argv = []string{"nc"}
	withArgv.TimeoutSeconds = 1
	if _, err := normalizeProjectNetworkRequest(OperationProjectNetworkRoute, withArgv); err == nil {
		t.Fatal("network route accepted executable fields")
	}
}

func TestNormalizeProjectNetworkProbeSortsAndBoundsPorts(t *testing.T) {
	request := projectNetworkRequestFixture()
	request.NetworkPorts = []int{443, 22, 80}
	request.NetworkTimeoutMillis = 500
	normalized, err := normalizeProjectNetworkRequest(OperationProjectNetworkProbe, request)
	if err != nil || len(normalized.NetworkPorts) != 3 || normalized.NetworkPorts[0] != 22 || normalized.NetworkPorts[1] != 80 || normalized.NetworkPorts[2] != 443 {
		t.Fatalf("normalized=%+v err=%v", normalized, err)
	}
	for _, ports := range [][]int{{}, {22, 22}, {0}, {65536}} {
		invalid := request
		invalid.NetworkPorts = ports
		if _, err := normalizeProjectNetworkRequest(OperationProjectNetworkProbe, invalid); err == nil {
			t.Fatalf("accepted ports %v", ports)
		}
	}
	for _, timeout := range []int{49, 1501} {
		invalid := request
		invalid.NetworkTimeoutMillis = timeout
		if _, err := normalizeProjectNetworkRequest(OperationProjectNetworkProbe, invalid); err == nil {
			t.Fatalf("accepted timeout %d", timeout)
		}
	}
}

func TestProjectNetworkResultValidationIsKindSpecificAndVPNBound(t *testing.T) {
	route := projectNetworkResultFixture()
	if !validProjectNetworkResultForKind(OperationProjectNetworkRoute, route) {
		t.Fatalf("route result rejected: %+v", route)
	}
	probe := projectNetworkResultFixture()
	probe.NetworkPorts = []NetworkPortResult{{Port: 22, State: "open"}, {Port: 80, State: "closed"}, {Port: 443, State: "filtered"}}
	if !validProjectNetworkResultForKind(OperationProjectNetworkProbe, probe) {
		t.Fatalf("probe result rejected: %+v", probe)
	}
	if validProjectNetworkResultForKind(OperationProjectNetworkRoute, probe) {
		t.Fatal("probe result accepted as route result")
	}
	badInterface := route
	badInterface.NetworkInterface = "eth0"
	if validProjectNetworkResultForKind(OperationProjectNetworkRoute, badInterface) {
		t.Fatal("non-VPN result accepted")
	}
	badOrder := probe
	badOrder.NetworkPorts = []NetworkPortResult{{Port: 80, State: "open"}, {Port: 22, State: "open"}}
	if validProjectNetworkResultForKind(OperationProjectNetworkProbe, badOrder) {
		t.Fatal("unsorted result accepted")
	}
	badState := probe
	badState.NetworkPorts[0].State = "unknown"
	if validProjectNetworkResultForKind(OperationProjectNetworkProbe, badState) {
		t.Fatal("unknown port state accepted")
	}
}
