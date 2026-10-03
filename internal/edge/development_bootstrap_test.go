package edge

import (
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func bootstrapBindingFixture() *ProjectDevelopmentBootstrapBinding {
	return &ProjectDevelopmentBootstrapBinding{Version: 1, CapabilityID: "toolchain.go.v1-26", Anchor: development.WorkspaceAnchor{
		DeviceID: "ed_" + strings.Repeat("a", 32), WorkspaceID: "ws_" + strings.Repeat("b", 32), Generation: 1, Owner: "charle-z", Repository: "project"}}
}

func TestDevelopmentBootstrapRequestHasNoExecutableAuthority(t *testing.T) {
	request := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "bootstrap-resolve",
		DevelopmentBootstrap: bootstrapBindingFixture()}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectDevelopmentBootstrapResolve, request); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*OperationRequest){
		func(r *OperationRequest) { r.Argv = []string{"sh", "-c", "id"} },
		func(r *OperationRequest) { r.Environment = map[string]string{"PATH": "/host"} },
		func(r *OperationRequest) { r.ToolboxServiceName = "docker" },
		func(r *OperationRequest) { r.DevelopmentBootstrap.CapabilityID = "toolchain.rust.vstable" },
		func(r *OperationRequest) {
			r.DevelopmentBootstrap.ResolutionDigest = "sha256:" + strings.Repeat("a", 64)
		},
	} {
		forged := request
		forged.DevelopmentBootstrap = bootstrapBindingFixture()
		mutate(&forged)
		if _, err := validateOperationRequestWithProjectExec(OperationProjectDevelopmentBootstrapResolve, forged); err == nil {
			t.Fatal("accepted foreign bootstrap authority")
		}
	}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectStatus, request); err == nil {
		t.Fatal("normal operation accepted bootstrap binding")
	}
}

func TestDevelopmentBootstrapStartBindsExactOfficialSelection(t *testing.T) {
	binding := bootstrapBindingFixture()
	binding.Resolution = &development.BootstrapResolution{CapabilityID: binding.CapabilityID, Toolchain: "go", Version: "1.26.6", Platform: "amd64",
		ArtifactFile: "go1.26.6.linux-amd64.tar.gz", ArtifactSHA256: strings.Repeat("c", 64), ArtifactSize: 1024}
	binding.ResolutionDigest, _ = development.BootstrapResolutionDigest(*binding.Resolution)
	request := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "bootstrap-start", DevelopmentBootstrap: binding}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectDevelopmentBootstrapStart, request); err != nil {
		t.Fatal(err)
	}
	binding.Resolution.Version = "1.26.7"
	binding.Resolution.ArtifactFile = "go1.26.7.linux-amd64.tar.gz"
	if _, err := validateOperationRequestWithProjectExec(OperationProjectDevelopmentBootstrapStart, request); err == nil {
		t.Fatal("retry substituted a newer resolution")
	}
}

func TestDevelopmentBootstrapResolveAcceptsCapturedRegistrationOnly(t *testing.T) {
	binding := bootstrapBindingFixture()
	binding.Resolution = &development.BootstrapResolution{CapabilityID: binding.CapabilityID, Toolchain: "go", Version: "1.26.6", Platform: "amd64", ArtifactFile: "go1.26.6.linux-amd64.tar.gz", ArtifactSHA256: strings.Repeat("c", 64), ArtifactSize: 1024}
	binding.ResolutionDigest, _ = development.BootstrapResolutionDigest(*binding.Resolution)
	result := OperationResult{WorkspaceID: binding.Anchor.WorkspaceID, ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "project", ProjectTarget: "parrot", ProjectState: "registered", ProjectProfile: "linux-workcell", ProjectMode: "dev", DevelopmentBootstrap: binding}
	if !validOperationCompletionForKind(OperationProjectDevelopmentBootstrapResolve, result, "") {
		t.Fatal("registry-only bootstrap resolution rejected as operation_result_invalid")
	}
	if validOperationCompletionForKind(OperationProjectDevelopmentInspect, result, "") {
		t.Fatal("bootstrap registration masqueraded as inspected environment")
	}
}
