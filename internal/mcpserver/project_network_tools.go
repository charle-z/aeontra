package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectNetworkParams struct {
	Alias       string `json:"alias"`
	Target      string `json:"target"`
	Destination string `json:"destination"`
	Ports       []int  `json:"ports,omitempty"`
	TimeoutMS   int    `json:"timeout_ms,omitempty"`
}

type projectNetworkPublicView struct {
	OperationID string                   `json:"operation_id"`
	State       edge.OperationState      `json:"state"`
	Alias       string                   `json:"alias"`
	Repository  string                   `json:"repository,omitempty"`
	Target      string                   `json:"target"`
	Profile     string                   `json:"profile,omitempty"`
	Mode        string                   `json:"mode,omitempty"`
	Destination string                   `json:"destination,omitempty"`
	Interface   string                   `json:"interface,omitempty"`
	SourceIP    string                   `json:"source_ip,omitempty"`
	Ports       []edge.NetworkPortResult `json:"ports,omitempty"`
	Reason      string                   `json:"reason,omitempty"`
}

func (s *Server) addProjectNetworkTools(projectSchema map[string]any) {
	hints := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}
	base := map[string]any{
		"alias":       projectSchema["alias"],
		"target":      projectSchema["target"],
		"destination": stringSchema("one private IPv4 destination reached through a VPN interface", `^[0-9.]{7,15}$`, 15),
	}
	s.addDirectTool(toolDef{
		Name:        "project_network_route",
		Description: "Resolve the route from the selected trusted Edge workcell to one private IPv4 destination. The Edge accepts only routes through tun*/tap* VPN interfaces and returns structured interface/source metadata; no command or shell input is accepted.",
		InputSchema: closedObject(base, []string{"alias", "target", "destination"}), Version: "1", Annotations: hints,
	}, func(arguments json.RawMessage) (string, error) {
		return s.handleProjectNetwork(arguments, edge.OperationProjectNetworkRoute)
	})

	probe := map[string]any{}
	for key, value := range base {
		probe[key] = value
	}
	probe["ports"] = map[string]any{
		"type": "array", "minItems": 1, "maxItems": edge.MaxProjectNetworkPorts, "uniqueItems": true,
		"items": map[string]any{"type": "integer", "minimum": 1, "maximum": 65535},
	}
	probe["timeout_ms"] = map[string]any{"type": "integer", "minimum": edge.MinProjectNetworkTimeoutMillis, "maximum": edge.MaxProjectNetworkTimeoutMillis}
	s.addDirectTool(toolDef{
		Name:        "project_network_probe",
		Description: "Perform bounded TCP connect probes from the selected trusted Edge workcell to explicit ports on one private IPv4 destination. The Edge requires a tun*/tap* VPN route and accepts no executable, script, URL, credential, or shell input.",
		InputSchema: closedObject(probe, []string{"alias", "target", "destination", "ports", "timeout_ms"}), Version: "1", Annotations: hints,
	}, func(arguments json.RawMessage) (string, error) {
		return s.handleProjectNetwork(arguments, edge.OperationProjectNetworkProbe)
	})
}

func (s *Server) handleProjectNetwork(arguments json.RawMessage, kind edge.OperationKind) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	var params projectNetworkParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	device, err := resolver.ResolveActiveDeviceName(params.Target)
	if err != nil {
		return "", err
	}
	request := edge.OperationRequest{
		Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell",
		NetworkDestination: params.Destination, NetworkPorts: params.Ports, NetworkTimeoutMillis: params.TimeoutMS,
	}
	operation, _, err := s.edgeOperations.CreateOperation(device.ID, kind, request)
	if err == nil {
		operation, err = s.edgeOperations.WaitOperation(context.Background(), operation.ID, 180*time.Second)
	}
	view := projectNetworkPublicView{
		OperationID: operation.ID, State: operation.State, Alias: params.Alias, Target: params.Target,
		Destination: params.Destination,
	}
	if operation.State == edge.OperationSucceeded {
		view.Alias = operation.Result.ProjectAlias
		view.Repository = operation.Result.ProjectOwner + "/" + operation.Result.ProjectRepository
		view.Target = operation.Result.ProjectTarget
		view.Profile = operation.Result.ProjectProfile
		view.Mode = operation.Result.ProjectMode
		view.Destination = operation.Result.NetworkDestination
		view.Interface = operation.Result.NetworkInterface
		view.SourceIP = operation.Result.NetworkSourceIP
		view.Ports = operation.Result.NetworkPorts
	} else if operation.State == edge.OperationFailed || operation.State == edge.OperationCancelled {
		view.Reason = operation.SafeCode
	}
	return marshalToolValue(view, err)
}
