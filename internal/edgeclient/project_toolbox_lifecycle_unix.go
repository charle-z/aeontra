//go:build !windows

package edgeclient

func projectToolboxReclaimability(record projectToolboxRecord, state ProjectToolboxState) (bool, string) {
	if record.Lifecycle != projectToolboxDisposable {
		return false, "persistent"
	}
	switch state {
	case ProjectToolboxRunning, ProjectToolboxCreated:
		return false, "active"
	case ProjectToolboxUnknown:
		return false, "unknown_state"
	case ProjectToolboxStopped:
	default:
		return false, "unknown_state"
	}
	for _, service := range record.Services {
		if service.State != "stopped" {
			return false, "service_active"
		}
	}
	for _, run := range record.BrowserHarnessRuns {
		if !browserHarnessTerminal(run.State) {
			return false, "browser_active"
		}
	}
	return true, "eligible"
}
