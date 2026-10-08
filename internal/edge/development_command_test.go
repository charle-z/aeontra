package edge

import (
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func developmentCommandFixture(t *testing.T) OperationRequest {
	t.Helper()
	digest, err := development.CommandDigest([]string{"make", "validate-all"})
	if err != nil {
		t.Fatal(err)
	}
	return OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "development-attempt-one", Argv: []string{"make", "validate-all"},
		DevelopmentCommand: &ProjectDevelopmentCommandBinding{Version: 1,
			Anchor:       development.WorkspaceAnchor{DeviceID: "ed_" + strings.Repeat("a", 32), WorkspaceID: "ws_" + strings.Repeat("b", 32), Generation: 1, Owner: "charle-z", Repository: "project"},
			SourceDigest: "sha256:" + strings.Repeat("a", 64), EnvironmentDigest: "sha256:" + strings.Repeat("b", 64), CommandDigest: digest,
			PrivateBodyRef: "mb_" + strings.Repeat("c", 32), PrivateBodyDigest: "sha256:" + strings.Repeat("d", 64), Requirements: []development.CapabilityID{"build.make"}, TimeoutSeconds: 3600}}
}

func TestDevelopmentCommandPrivateBindingIsClosed(t *testing.T) {
	request := developmentCommandFixture(t)
	if _, err := validateOperationRequestWithProjectExec(OperationProjectDevelopmentCommandStart, request); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []OperationKind{OperationProjectProcessStart, OperationProjectExec, OperationProjectStatus, OperationProjectDevelopmentInspect} {
		if _, err := validateOperationRequestWithProjectExec(kind, request); err == nil {
			t.Fatalf("accepted private binding via %s", kind)
		}
	}
	for _, mutate := range []func(*OperationRequest){
		func(r *OperationRequest) { r.Argv = []string{"make", "test"} },
		func(r *OperationRequest) { r.DevelopmentCommand.Anchor.Generation = 0 },
		func(r *OperationRequest) { r.DevelopmentCommand.TimeoutSeconds = 86401 },
		func(r *OperationRequest) { r.WorkspaceID = r.DevelopmentCommand.Anchor.WorkspaceID },
		func(r *OperationRequest) {
			r.DevelopmentCommand.Requirements = []development.CapabilityID{"build.make", "build.make"}
		},
		func(r *OperationRequest) { r.BackgroundProcessID = "pr_" + strings.Repeat("a", 32) },
		func(r *OperationRequest) { r.ToolboxServiceName = "docker" },
		func(r *OperationRequest) { r.BrowserSessionID = "bs_" + strings.Repeat("a", 32) },
		func(r *OperationRequest) { r.DevelopmentCommand.PrivateBodyRef = "file:///token" },
	} {
		forged := developmentCommandFixture(t)
		mutate(&forged)
		if _, err := validateOperationRequestWithProjectExec(OperationProjectDevelopmentCommandStart, forged); err == nil {
			t.Fatal("accepted invalid development command")
		}
	}
}

func TestNormalizeDevelopmentCommandRejectsForeignOperationFields(t *testing.T) {
	request := developmentCommandFixture(t)
	request.DevelopmentCommand = nil
	request.TimeoutSeconds = 3600
	if _, err := NormalizeDevelopmentCommand(request); err != nil {
		t.Fatal(err)
	}
	request.ToolboxServiceName = "docker"
	if _, err := NormalizeDevelopmentCommand(request); err == nil {
		t.Fatal("accepted toolbox authority in command request")
	}
}

func TestDevelopmentRecoveryIsPrivateAndNeverNormalCommandAuthority(t *testing.T) {
	request := developmentCommandFixture(t)
	request.DevelopmentRecoveryOperationID = "eo_" + strings.Repeat("c", 32)
	request.DevelopmentRecoveryIdempotencyKey = "original-command-key"
	normalized, err := validateOperationRequestWithProjectExec(OperationProjectDevelopmentCommandStart, request)
	if err != nil || normalized.DevelopmentRecoveryOperationID != request.DevelopmentRecoveryOperationID || normalized.DevelopmentRecoveryIdempotencyKey != request.DevelopmentRecoveryIdempotencyKey {
		t.Fatalf("valid recovery identity rejected: %v", err)
	}
	for _, mutation := range []func(*OperationRequest){
		func(r *OperationRequest) { r.DevelopmentRecoveryOperationID = "" },
		func(r *OperationRequest) { r.DevelopmentRecoveryIdempotencyKey = "" },
		func(r *OperationRequest) { r.DevelopmentRecoveryOperationID = "eo_other" },
		func(r *OperationRequest) { r.DevelopmentRecoveryIdempotencyKey = r.IdempotencyKey },
	} {
		bad := request
		mutation(&bad)
		if _, err := validateOperationRequestWithProjectExec(OperationProjectDevelopmentCommandStart, bad); err == nil {
			t.Fatal("invalid recovery pair accepted")
		}
	}
	request.DevelopmentCommand = nil
	if _, err := validateOperationRequestWithProjectExec(OperationProjectProcessStart, request); err == nil {
		t.Fatal("recovery fields leaked into normal process authority")
	}
	request.TimeoutSeconds = 3600
	if _, err := NormalizeDevelopmentCommand(request); err == nil {
		t.Fatal("public command normalized a recovery identity")
	}
}

func TestDevelopmentCommandReservesFixedTimeoutOverhead(t *testing.T) {
	request := developmentCommandFixture(t)
	request.DevelopmentCommand = nil
	request.TimeoutSeconds = 3600
	request.Argv = make([]string, 124)
	for i := range request.Argv {
		request.Argv[i] = "argument"
	}
	if _, err := NormalizeDevelopmentCommand(request); err != nil {
		t.Fatalf("bounded wrapped command rejected: %v", err)
	}
	request.Argv = append(request.Argv, "extra")
	if _, err := NormalizeDevelopmentCommand(request); err == nil {
		t.Fatal("accepted a command exceeding executor limits after fixed timeout wrapper")
	}
}

func TestDevelopmentCommandResultCannotMasqueradeAsNormalProcess(t *testing.T) {
	request := developmentCommandFixture(t)
	result := OperationResult{WorkspaceID: request.DevelopmentCommand.Anchor.WorkspaceID, ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "project", ProjectTarget: "parrot", ProjectState: "dirty", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		BackgroundProcessID: "pr_" + strings.Repeat("a", 32), BackgroundProcessState: "running", BackgroundStartedAt: time.Now().UTC().Format(time.RFC3339Nano), DevelopmentCommand: request.DevelopmentCommand}
	if !validOperationCompletionForKind(OperationProjectDevelopmentCommandStart, result, "") {
		t.Fatal("valid private process result rejected")
	}
	if validOperationCompletionForKind(OperationProjectProcessStart, result, "") {
		t.Fatal("normal process accepted private command receipt")
	}
	result.WorkspaceID = "ws_" + strings.Repeat("a", 32)
	if validOperationCompletionForKind(OperationProjectDevelopmentCommandStart, result, "") {
		t.Fatal("cross-workspace receipt accepted")
	}
}
