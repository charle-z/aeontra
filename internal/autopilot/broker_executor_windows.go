//go:build windows

package autopilot

import (
	"context"
	"errors"
)

type BrokerExecutor struct{ SocketPath, Workspace, WorkspaceID string }

func (BrokerExecutor) Validate(context.Context, string) error {
	return errors.New("local broker requires Linux")
}
func (BrokerExecutor) Execute(context.Context, LocalAgentResponse) (ActionObservation, error) {
	return ActionObservation{}, errors.New("local broker requires Linux")
}
