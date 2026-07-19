//go:build windows

package edgeclient

import (
	"context"
	"errors"
)

type HTBLabBrokerConfig struct {
	SocketPath string
	StateRoot  string
	Workspace  Workspace
	RuntimeID  string
	ToolPath   string
	Probe      LinuxNetworkProbe
}

func StartHTBLabBroker(context.Context, HTBLabBrokerConfig) (<-chan error, error) {
	return nil, errors.New("HTB lab broker requires Linux")
}
