package mcpserver

import (
	"encoding/json"
	"errors"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type edgeConnectivityReader interface {
	DeviceConnectivity(string) (edge.DeviceConnectivity, error)
}

func (s *Server) handleEdgeConnectivityStatus(arguments json.RawMessage) (string, error) {
	if s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	var params struct {
		Target   string `json:"target"`
		DeviceID string `json:"device_id"`
	}
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	if (params.Target == "") == (params.DeviceID == "") {
		return "", errors.New("supply exactly one target or device_id")
	}
	if params.Target != "" {
		resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
		if !ok {
			return "", errors.New("edge target alias resolution is unavailable")
		}
		device, err := resolver.ResolveActiveDeviceName(params.Target)
		if err != nil {
			return "", err
		}
		params.DeviceID = device.ID
	}
	if !s.edgeDevices.DeviceActive(params.DeviceID) {
		return "", errors.New("active edge device not found")
	}
	reader, ok := s.edgeDevices.(edgeConnectivityReader)
	if !ok {
		return "", errors.New("edge connectivity metadata is unavailable")
	}
	view, err := reader.DeviceConnectivity(params.DeviceID)
	return marshalToolValue(view, err)
}
