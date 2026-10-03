package edge

import (
	"strings"
	"testing"
)

func developmentCommandAbsenceFixture(t *testing.T) OperationResult {
	t.Helper()
	request := developmentCommandFixture(t)
	return OperationResult{DevelopmentCommandAbsence: &ProjectDevelopmentCommandAbsence{
		Version: 1, OriginalOperationID: "eo_" + strings.Repeat("d", 32),
		OriginalIdempotencyKey: request.IdempotencyKey, Command: *request.DevelopmentCommand,
	}}
}

func TestDevelopmentCommandAbsenceCompletionIsClosed(t *testing.T) {
	result := developmentCommandAbsenceFixture(t)
	if !validOperationCompletionForKind(OperationProjectDevelopmentCommandStart, result, DevelopmentCommandEffectAbsentSafeCode) {
		t.Fatal("authenticated command absence receipt rejected")
	}
	for _, kind := range []OperationKind{OperationProjectProcessStart, OperationProjectDevelopmentInspect, OperationProjectDevelopmentBootstrapStart} {
		if validOperationCompletionForKind(kind, result, DevelopmentCommandEffectAbsentSafeCode) {
			t.Fatalf("absence accepted through unrelated kind %s", kind)
		}
	}
	for _, code := range []string{"", "project_development_reconciliation_required", "operation_execution_interrupted"} {
		if validOperationCompletionForKind(OperationProjectDevelopmentCommandStart, result, code) {
			t.Fatalf("absence accepted with unrelated code %q", code)
		}
	}
	for _, mutate := range []func(*OperationResult){
		func(r *OperationResult) { r.DevelopmentCommandAbsence = nil },
		func(r *OperationResult) { r.DevelopmentCommandAbsence.Version = 2 },
		func(r *OperationResult) { r.DevelopmentCommandAbsence.OriginalOperationID = "eo_other" },
		func(r *OperationResult) { r.DevelopmentCommandAbsence.OriginalIdempotencyKey = "" },
		func(r *OperationResult) { r.DevelopmentCommandAbsence.Command.Anchor.Generation = 0 },
		func(r *OperationResult) { r.BackgroundProcessID = "pr_" + strings.Repeat("a", 32) },
		func(r *OperationResult) { r.BackgroundExitKnown = true },
		func(r *OperationResult) { r.DevelopmentCommand = &r.DevelopmentCommandAbsence.Command },
		func(r *OperationResult) { r.DevelopmentInspection = &ProjectDevelopmentInspection{} },
		func(r *OperationResult) { r.DevelopmentBootstrap = &ProjectDevelopmentBootstrapBinding{} },
		func(r *OperationResult) { r.ProjectAlias = "foreign" },
		func(r *OperationResult) { r.JobID = "unrelated" },
	} {
		forged := developmentCommandAbsenceFixture(t)
		mutate(&forged)
		if validOperationCompletionForKind(OperationProjectDevelopmentCommandStart, forged, DevelopmentCommandEffectAbsentSafeCode) {
			t.Fatal("accepted malformed or conflicting absence receipt")
		}
	}
}
