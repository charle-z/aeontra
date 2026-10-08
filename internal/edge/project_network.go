package edge

import (
	"errors"
	"net"
	"regexp"
	"sort"
	"strings"
)

const (
	MaxProjectNetworkPorts         = 64
	MinProjectNetworkTimeoutMillis = 50
	MaxProjectNetworkTimeoutMillis = 1500
)

var projectNetworkInterfacePattern = regexp.MustCompile(`^(?:tun|tap)[A-Za-z0-9_.:-]{0,31}$`)
var projectNetworkPortStatePattern = regexp.MustCompile(`^(?:open|closed|filtered|unreachable|error)$`)

func normalizeProjectNetworkRequest(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	if !emptyProjectExecRequestFields(request) || !emptyProjectProcessRequestFields(request) || request.GitPlanID != "" {
		return OperationRequest{}, errors.New("project network request is invalid")
	}
	request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
	request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
	request.Profile = strings.TrimSpace(request.Profile)
	destination := strings.TrimSpace(request.NetworkDestination)
	parsed := net.ParseIP(destination)
	if !projectOperationAliasPattern.MatchString(request.Alias) || !projectOperationTargetPattern.MatchString(request.TargetAlias) ||
		request.Profile != "linux-workcell" || request.Repository != "" || request.Platform != "" || request.Machine != "" ||
		request.Target != "" || request.Difficulty != "" || request.OperatingSystem != "" || request.WorkspaceID != "" ||
		request.RunUntil != "" || request.Release != "" || request.IdempotencyKey != "" || parsed == nil || parsed.To4() == nil ||
		!parsed.IsPrivate() || strings.Contains(destination, "/") {
		return OperationRequest{}, errors.New("project network request is invalid")
	}
	request.NetworkDestination = parsed.To4().String()

	switch kind {
	case OperationProjectNetworkRoute:
		if len(request.NetworkPorts) != 0 || request.NetworkTimeoutMillis != 0 {
			return OperationRequest{}, errors.New("project network route request is invalid")
		}
	case OperationProjectNetworkProbe:
		if len(request.NetworkPorts) == 0 || len(request.NetworkPorts) > MaxProjectNetworkPorts ||
			request.NetworkTimeoutMillis < MinProjectNetworkTimeoutMillis || request.NetworkTimeoutMillis > MaxProjectNetworkTimeoutMillis {
			return OperationRequest{}, errors.New("project network probe request is invalid")
		}
		ports := append([]int(nil), request.NetworkPorts...)
		sort.Ints(ports)
		for index, port := range ports {
			if port < 1 || port > 65535 || (index > 0 && port == ports[index-1]) {
				return OperationRequest{}, errors.New("project network probe ports are invalid")
			}
		}
		request.NetworkPorts = ports
	default:
		return OperationRequest{}, errors.New("project network operation is invalid")
	}
	return request, nil
}

func emptyProjectNetworkRequestFields(request OperationRequest) bool {
	return request.NetworkDestination == "" && len(request.NetworkPorts) == 0 && request.NetworkTimeoutMillis == 0
}

func hasProjectNetworkResult(result OperationResult) bool {
	return result.NetworkDestination != "" || result.NetworkInterface != "" || result.NetworkSourceIP != "" || len(result.NetworkPorts) != 0
}

func validProjectNetworkResultForKind(kind OperationKind, result OperationResult) bool {
	destination := net.ParseIP(result.NetworkDestination)
	source := net.ParseIP(result.NetworkSourceIP)
	if destination == nil || destination.To4() == nil || !destination.IsPrivate() ||
		result.NetworkDestination != destination.To4().String() || !projectNetworkInterfacePattern.MatchString(result.NetworkInterface) ||
		source == nil || source.To4() == nil || result.NetworkSourceIP != source.To4().String() {
		return false
	}

	switch kind {
	case OperationProjectNetworkRoute:
		if len(result.NetworkPorts) != 0 {
			return false
		}
	case OperationProjectNetworkProbe:
		if len(result.NetworkPorts) == 0 || len(result.NetworkPorts) > MaxProjectNetworkPorts {
			return false
		}
		previous := 0
		for _, item := range result.NetworkPorts {
			if item.Port < 1 || item.Port > 65535 || item.Port <= previous || !projectNetworkPortStatePattern.MatchString(item.State) {
				return false
			}
			previous = item.Port
		}
	default:
		return false
	}

	metadata := result
	metadata.NetworkDestination = ""
	metadata.NetworkInterface = ""
	metadata.NetworkSourceIP = ""
	metadata.NetworkPorts = nil
	return validProjectOperationResult(metadata)
}
