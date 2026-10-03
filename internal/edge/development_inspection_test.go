package edge

import (
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func TestProjectDevelopmentInspectionIsScopedAndCannotClaimExternalAuthority(t *testing.T) {
	workspace := "ws_" + strings.Repeat("a", 32)
	caps, err := development.NewCapabilitySet("toolchain.go")
	if err != nil {
		t.Fatal(err)
	}
	environment, err := development.NewEnvironmentAttestation("workcell:"+workspace, development.ClassWorkcell, 1, caps)
	if err != nil {
		t.Fatal(err)
	}
	record, err := environment.Record()
	if err != nil {
		t.Fatal(err)
	}
	result := OperationResult{WorkspaceID: workspace, ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo",
		ProjectTarget: "parrot", ProjectState: "dirty", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		DevelopmentInspection: &ProjectDevelopmentInspection{Version: 1, ProjectGeneration: 1, SourceDigest: "sha256:" + strings.Repeat("a", 64), Requirements: []development.CapabilityID{"toolchain.go"}, Environments: []development.EnvironmentRecord{record}}}
	if !validOperationCompletionForKind(OperationProjectDevelopmentInspect, result, "") {
		t.Fatal("valid dirty-workspace inspection was rejected")
	}
	result.DevelopmentInspection.SourceEvidenceKnown = true
	result.DevelopmentInspection.SourceHead = strings.Repeat("d", 40)
	if !validOperationCompletionForKind(OperationProjectDevelopmentInspect, result, "") {
		t.Fatal("exact committed source evidence rejected")
	}
	if validOperationCompletionForKind(OperationProjectStatus, result, "") {
		t.Fatal("inspection was accepted from a different operation kind")
	}
	for _, mutate := range []func(*OperationResult){
		func(r *OperationResult) { r.WorkspaceID = "ws_" + strings.Repeat("b", 32) },
		func(r *OperationResult) { r.DevelopmentInspection.ProjectGeneration++ },
		func(r *OperationResult) {
			r.DevelopmentInspection.Environments[0].Capabilities = []development.CapabilityID{"host.root"}
		},
		func(r *OperationResult) {
			vm, _ := development.NewEnvironmentAttestation("runner:external", development.ClassIsolatedRunner, 1, caps)
			r.DevelopmentInspection.Environments[0], _ = vm.Record()
		},
		func(r *OperationResult) { r.ExecCompleted = true },
		func(r *OperationResult) { r.DevelopmentInspection.SourceHead = "main" },
		func(r *OperationResult) { r.DevelopmentInspection.SourceEvidenceKnown = false },
	} {
		forged := result
		inspection := *result.DevelopmentInspection
		inspection.Environments = append([]development.EnvironmentRecord(nil), inspection.Environments...)
		forged.DevelopmentInspection = &inspection
		mutate(&forged)
		if validOperationCompletionForKind(OperationProjectDevelopmentInspect, forged, "") {
			t.Fatal("forged inspection was accepted")
		}
	}
	request := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "inspect-one"}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectDevelopmentInspect, request); err != nil {
		t.Fatal(err)
	}
	request.Argv = []string{"sudo", "true"}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectDevelopmentInspect, request); err == nil {
		t.Fatal("inspection accepted an arbitrary command")
	}
}
