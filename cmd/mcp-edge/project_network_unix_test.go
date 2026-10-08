//go:build !windows

package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type projectNetworkTimeoutError struct{}

func (projectNetworkTimeoutError) Error() string   { return "timeout" }
func (projectNetworkTimeoutError) Timeout() bool   { return true }
func (projectNetworkTimeoutError) Temporary() bool { return true }

func projectNetworkResolutionFixture(t *testing.T) edgeclient.ProjectResolution {
	t.Helper()
	workspacePath := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	return edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "mcp-devbox", Owner: "charle-z", Repository: "mcp-devbox"},
		TargetAlias: "parrot-trusted-linux",
		Workspace: edgeclient.Workspace{
			ID: "ws_0123456789abcdef0123456789abcdef", Path: workspacePath,
			Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev,
		},
	}
}

func TestCollectProjectNetworkRouteDoesNotDial(t *testing.T) {
	resolved := projectNetworkResolutionFixture(t)
	operation := edge.Operation{Kind: edge.OperationProjectNetworkRoute, Request: edge.OperationRequest{NetworkDestination: "10.64.140.182"}}
	dials := 0
	result, code := collectProjectNetwork(context.Background(), operation, resolved,
		func(context.Context, string) (string, string, error) { return "tun0", "192.168.165.249", nil },
		func(context.Context, string, int, time.Duration) error { dials++; return nil },
	)
	if code != "" || dials != 0 || result.NetworkDestination != "10.64.140.182" || result.NetworkInterface != "tun0" || result.NetworkSourceIP != "192.168.165.249" || len(result.NetworkPorts) != 0 {
		t.Fatalf("result=%+v code=%q dials=%d", result, code, dials)
	}
	if result.ProjectAlias != "mcp-devbox" || result.ProjectMode != "dev" || result.ProjectProfile != "linux-workcell" {
		t.Fatalf("project metadata=%+v", result)
	}
}

func TestCollectProjectNetworkProbeReturnsStructuredStates(t *testing.T) {
	resolved := projectNetworkResolutionFixture(t)
	operation := edge.Operation{Kind: edge.OperationProjectNetworkProbe, Request: edge.OperationRequest{
		NetworkDestination: "10.64.140.182", NetworkPorts: []int{21, 22, 80, 443}, NetworkTimeoutMillis: 250,
	}}
	seen := []int{}
	result, code := collectProjectNetwork(context.Background(), operation, resolved,
		func(context.Context, string) (string, string, error) { return "tun0", "192.168.165.249", nil },
		func(_ context.Context, _ string, port int, timeout time.Duration) error {
			seen = append(seen, port)
			if timeout != 250*time.Millisecond {
				t.Fatalf("timeout=%s", timeout)
			}
			switch port {
			case 21:
				return nil
			case 22:
				return syscall.ECONNREFUSED
			case 80:
				return projectNetworkTimeoutError{}
			default:
				return syscall.ENETUNREACH
			}
		},
	)
	want := []edge.NetworkPortResult{{Port: 21, State: "open"}, {Port: 22, State: "closed"}, {Port: 80, State: "filtered"}, {Port: 443, State: "unreachable"}}
	if code != "" || !reflect.DeepEqual(result.NetworkPorts, want) || !reflect.DeepEqual(seen, []int{21, 22, 80, 443}) {
		t.Fatalf("result=%+v code=%q seen=%v", result, code, seen)
	}
}

func TestCollectProjectNetworkFailsClosedForWorkspaceAndRoute(t *testing.T) {
	resolved := projectNetworkResolutionFixture(t)
	operation := edge.Operation{Kind: edge.OperationProjectNetworkRoute, Request: edge.OperationRequest{NetworkDestination: "10.64.140.182"}}
	resolved.Workspace.Mode = edgeclient.WorkspaceModeHTBLinux
	result, code := collectProjectNetwork(context.Background(), operation, resolved,
		func(context.Context, string) (string, string, error) { return "tun0", "192.168.165.249", nil },
		func(context.Context, string, int, time.Duration) error { return nil },
	)
	if code != "project_network_invalid" || !reflect.DeepEqual(result, edge.OperationResult{}) {
		t.Fatalf("wrong-mode result=%+v code=%q", result, code)
	}
	resolved.Workspace.Mode = edgeclient.WorkspaceModeDev
	result, code = collectProjectNetwork(context.Background(), operation, resolved,
		func(context.Context, string) (string, string, error) { return "", "", errors.New("no vpn route") },
		func(context.Context, string, int, time.Duration) error { return nil },
	)
	if code != "project_network_route_failed" || !reflect.DeepEqual(result, edge.OperationResult{}) {
		t.Fatalf("route-failure result=%+v code=%q", result, code)
	}
}

func TestClassifyProjectNetworkDial(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{{nil, "open"}, {syscall.ECONNREFUSED, "closed"}, {syscall.EHOSTUNREACH, "unreachable"}, {projectNetworkTimeoutError{}, "filtered"}, {errors.New("other"), "error"}}
	for _, test := range cases {
		if got := classifyProjectNetworkDial(test.err); got != test.want {
			t.Fatalf("classify(%v)=%q want=%q", test.err, got, test.want)
		}
	}
	var _ net.Error = projectNetworkTimeoutError{}
}
