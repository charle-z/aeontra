package edgeclient

import "strings"

func repositoryMismatchObservation(path, owner, repository, observed string) ProjectCheckoutObservation {
	return ProjectCheckoutObservation{State: ProjectCheckoutRemoteMismatch, Diagnostic: ProjectCheckoutDiagnostic{
		Reason: "repository_identity_mismatch", Path: sanitizeCheckoutValue(path), Expected: owner + "/" + repository,
		Observed: sanitizeCheckoutValue(strings.TrimSpace(observed)), Repairable: true, RecommendedAction: "project_reconcile",
	}}
}

func sanitizeCheckoutValue(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	if len(value) > 512 {
		return value[:512]
	}
	return value
}
