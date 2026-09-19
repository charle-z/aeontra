//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"strings"
)

type RootlessStorageDriverSnapshot struct {
	Engine  string
	Driver  string
	Options string
}

func InspectRootlessStorageDriver(ctx context.Context, endpoint *RootlessContainerEndpoint) (RootlessStorageDriverSnapshot, error) {
	if endpoint == nil || (endpoint.Engine != "podman" && endpoint.Engine != "docker") {
		return RootlessStorageDriverSnapshot{}, errors.New("rootless container endpoint is unavailable")
	}
	environment, err := rootlessContainerClientEnvironment(endpoint, openCodeDefaultToolPath)
	if err != nil {
		return RootlessStorageDriverSnapshot{}, err
	}
	prefix := rootlessEnginePrefix(endpoint)
	driverArgs := append(append([]string(nil), prefix...), "info", "--format")
	if endpoint.Engine == "podman" {
		driverArgs = append(driverArgs, "{{.Store.GraphDriverName}}")
	} else {
		driverArgs = append(driverArgs, "{{.Driver}}")
	}
	driverOutput, err := (execContainerCommandRunner{}).Run(ctx, endpoint.Executable, driverArgs, environment)
	if err != nil {
		return RootlessStorageDriverSnapshot{}, errors.New("rootless storage driver inspection failed")
	}
	driver := strings.TrimSpace(string(driverOutput))
	if driver == "" || len(driver) > 64 {
		return RootlessStorageDriverSnapshot{}, errors.New("rootless storage driver inspection failed")
	}
	options := ""
	if endpoint.Engine == "podman" {
		optionArgs := append(append([]string(nil), prefix...), "info", "--format", "{{.Store.GraphOptions}}")
		optionOutput, optionErr := (execContainerCommandRunner{}).Run(ctx, endpoint.Executable, optionArgs, environment)
		if optionErr == nil && len(optionOutput) <= 4096 {
			options = strings.TrimSpace(string(optionOutput))
		}
	}
	return RootlessStorageDriverSnapshot{Engine: endpoint.Engine, Driver: driver, Options: options}, nil
}
