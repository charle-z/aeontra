package sandboxexecutor

import "errors"

func NewPodmanEngine(string) (Engine, error) {
	return nil, errors.New("the private L3 rootless Podman executor is supported on Linux only")
}
