package edge

func validProjectToolboxLifecycle(lifecycle string) bool {
	return lifecycle == ProjectToolboxLifecyclePersistent || lifecycle == ProjectToolboxLifecycleDisposable
}

func validProjectToolboxLifecycleResult(result OperationResult) bool {
	if !validProjectToolboxLifecycle(result.ToolboxLifecycle) || result.ToolboxGeneration == 0 {
		return false
	}
	if result.ToolboxRemoved {
		return !result.ToolboxReclaimable && result.ToolboxReclaimReason == "removed"
	}
	if result.ToolboxLifecycle == ProjectToolboxLifecyclePersistent {
		return !result.ToolboxReclaimable && result.ToolboxReclaimReason == "persistent"
	}
	if result.ToolboxReclaimable {
		return result.ToolboxState == "stopped" && result.ToolboxReclaimReason == "eligible"
	}
	switch result.ToolboxReclaimReason {
	case "active":
		return result.ToolboxState == "running" || result.ToolboxState == "created"
	case "unknown_state":
		return result.ToolboxState == "unknown"
	case "service_active", "browser_active":
		return result.ToolboxState == "stopped"
	default:
		return false
	}
}
