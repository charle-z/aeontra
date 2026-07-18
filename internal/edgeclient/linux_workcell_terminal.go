package edgeclient

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func RecordLinuxWorkcellTerminalState(preparation *LinuxWorkcellPreparation, runtimeState, cleanupState string) error {
	if preparation == nil {
		return nil
	}
	runtimeState = strings.TrimSpace(runtimeState)
	cleanupState = strings.TrimSpace(cleanupState)
	if runtimeState == "" || cleanupState == "" {
		return errors.New("Linux workcell terminal state is invalid")
	}
	content, err := os.ReadFile(preparation.CurrentStatePath)
	if err != nil {
		return errors.New("Linux workcell current state is unavailable")
	}
	checkpoint := fmt.Sprintf(`

## Runtime terminal checkpoint

- Runtime state: %s
- Container cleanup: %s
- Active runtime process group: stopped
- Durable checkpoint: %s
`, runtimeState, cleanupState, preparation.CurrentStatePath)
	if int64(len(content)+len(checkpoint)) > linuxWorkcellStateLimit {
		return errors.New("Linux workcell current state limit would be exceeded")
	}
	return WriteLinuxWorkcellState(preparation.CurrentStatePath, string(content)+checkpoint)
}

func LinuxWorkcellContainerCleanupState(preparation *LinuxWorkcellPreparation, err error) string {
	if preparation == nil || preparation.RootlessContainer == nil {
		return "not-required"
	}
	if err != nil {
		return "pending: rootless resource cleanup failed"
	}
	return "complete: runtime-labelled resources removed"
}
