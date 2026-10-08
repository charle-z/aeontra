//go:build !windows

package main

import (
	"context"
	"errors"
	"net"
	"strconv"
	"syscall"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type projectNetworkRouteLookup func(context.Context, string) (string, string, error)
type projectNetworkDial func(context.Context, string, int, time.Duration) error

func executeProjectNetwork(ctx context.Context, stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
	_, workspaces, projects, _, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(ctx, operation.Request.Alias, operation.Request.TargetAlias)
	if err != nil {
		return edge.OperationResult{}, safeProjectControlFailure(err)
	}
	return collectProjectNetwork(ctx, operation, resolved, lookupProjectNetworkRoute, dialProjectNetworkTCP)
}

func collectProjectNetwork(ctx context.Context, operation edge.Operation, resolved edgeclient.ProjectResolution, route projectNetworkRouteLookup, dial projectNetworkDial) (edge.OperationResult, string) {
	if route == nil || dial == nil || resolved.Workspace.Profile != edgeclient.WorkspaceProfileLinuxWorkcell || resolved.Workspace.Mode != edgeclient.WorkspaceModeDev {
		return edge.OperationResult{}, "project_network_invalid"
	}
	request := operation.Request
	if net.ParseIP(request.NetworkDestination) == nil {
		return edge.OperationResult{}, "project_network_invalid"
	}
	iface, sourceIP, err := route(ctx, request.NetworkDestination)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return edge.OperationResult{}, "cancelled"
		}
		return edge.OperationResult{}, "project_network_route_failed"
	}
	result := edge.OperationResult{
		WorkspaceID:  resolved.Workspace.ID,
		ProjectAlias: resolved.Project.Alias, ProjectOwner: resolved.Project.Owner,
		ProjectRepository: resolved.Project.Repository, ProjectTarget: resolved.TargetAlias,
		ProjectState: resolved.SafeState(), ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode),
		NetworkDestination: request.NetworkDestination, NetworkInterface: iface, NetworkSourceIP: sourceIP,
	}
	if operation.Kind == edge.OperationProjectNetworkRoute {
		return result, ""
	}
	if operation.Kind != edge.OperationProjectNetworkProbe || len(request.NetworkPorts) == 0 {
		return edge.OperationResult{}, "project_network_invalid"
	}
	timeout := time.Duration(request.NetworkTimeoutMillis) * time.Millisecond
	result.NetworkPorts = make([]edge.NetworkPortResult, 0, len(request.NetworkPorts))
	for _, port := range request.NetworkPorts {
		err := dial(ctx, request.NetworkDestination, port, timeout)
		if ctx.Err() != nil {
			return edge.OperationResult{}, "cancelled"
		}
		result.NetworkPorts = append(result.NetworkPorts, edge.NetworkPortResult{Port: port, State: classifyProjectNetworkDial(err)})
	}
	return result, ""
}

func lookupProjectNetworkRoute(ctx context.Context, destination string) (string, string, error) {
	return detectLabRoute(ctx, destination, "")
}

func dialProjectNetworkTCP(ctx context.Context, destination string, port int, timeout time.Duration) error {
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "tcp4", net.JoinHostPort(destination, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	return connection.Close()
}

func classifyProjectNetworkDial(err error) string {
	if err == nil {
		return "open"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "closed"
	}
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return "unreachable"
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return "filtered"
	}
	return "error"
}
