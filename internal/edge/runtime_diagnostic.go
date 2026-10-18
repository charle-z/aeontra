package edge

import "regexp"

func validRuntimeDiagnosticState(result OperationResult) bool {
	hasProcessIdentity := regexp.MustCompile(`^p15\.[0-9]+\.[0-9]+$`).MatchString(result.ProcessRelease) && regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(result.ProcessCommit)
	noProcessIdentity := result.ProcessRelease == "" && result.ProcessCommit == ""
	if !hasProcessIdentity && !noProcessIdentity {
		return false
	}
	switch result.ProcessState {
	case "inactive":
		return !result.ServiceActive &&
			result.ServiceState == "inactive" &&
			(result.LockState == "missing" || result.LockState == "stale_recoverable") &&
			result.Coherence == "stopped" && noProcessIdentity
	case "single":
		if result.LockState != "held" || !hasProcessIdentity {
			return false
		}
		if result.Coherence == "managed" {
			return result.ServiceActive && result.ServiceState == "active"
		}
		return result.Coherence == "manual" && !result.ServiceActive && result.ServiceState == "inactive"
	case "duplicate":
		return result.ServiceActive && result.ServiceState == "active" &&
			result.LockState == "held" && result.Coherence == "duplicate" && hasProcessIdentity
	case "incoherent":
		return result.Coherence == "incoherent"
	default:
		return false
	}
}
