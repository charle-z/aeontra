package edge

import (
	"strings"
	"testing"
)

func TestProjectToolboxOperationContractsAreClosedAndIdempotent(t *testing.T) {
	common := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell"}
	for _, kind := range []OperationKind{OperationProjectToolboxCreate, OperationProjectToolboxCleanup} {
		request := common
		request.IdempotencyKey = "toolbox-1"
		if _, err := validateOperationRequestWithProjectExec(kind, request); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectToolboxStatus, common); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []OperationKind{OperationProjectToolboxExec, OperationProjectToolboxInstall} {
		request := common
		request.IdempotencyKey = "toolbox-exec-1"
		request.Argv = []string{"sh", "-lc", "apt-get update && apt-get install -y ruby"}
		request.CWD = "src"
		request.Environment = map[string]string{"CI": "true"}
		request.TimeoutSeconds = 1800
		if _, err := validateOperationRequestWithProjectExec(kind, request); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	invalid := common
	invalid.IdempotencyKey = "toolbox-exec-1"
	invalid.Argv = []string{"sh"}
	invalid.TimeoutSeconds = 3601
	if _, err := validateOperationRequestWithProjectExec(OperationProjectToolboxExec, invalid); err == nil {
		t.Fatal("accepted excessive toolbox timeout")
	}
	invalid.TimeoutSeconds = 60
	invalid.GitPlanID = "gp_" + strings.Repeat("1", 32)
	if _, err := validateOperationRequestWithProjectExec(OperationProjectToolboxExec, invalid); err == nil {
		t.Fatal("accepted cross-operation fields")
	}
}

func TestProjectToolboxCompletionIsBoundToItsOperationKind(t *testing.T) {
	result := OperationResult{
		WorkspaceID: "ws_11111111111111111111111111111111", ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo",
		ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		ToolboxID: "tb_22222222222222222222222222222222", ToolboxState: "running", ToolboxBase: "debian-bookworm-slim",
		ToolboxBaseImageID: "sha256:" + strings.Repeat("a", 64), ToolboxCreatedAt: "2026-08-02T12:00:00Z", ToolboxUpdatedAt: "2026-08-02T12:01:00Z",
	}
	if !validOperationCompletionForKind(OperationProjectToolboxStatus, result, "") {
		t.Fatal("valid toolbox status was rejected")
	}
	if validOperationCompletionForKind(OperationProjectExec, result, "") {
		t.Fatal("toolbox result masqueraded as project exec")
	}
	executed := result
	executed.ToolboxOutput = "ok\n"
	if !validOperationCompletionForKind(OperationProjectToolboxExec, executed, "") || validOperationCompletionForKind(OperationProjectToolboxStatus, executed, "") {
		t.Fatal("toolbox exec result kind validation failed")
	}
	cleaned := result
	cleaned.ToolboxState = "removed"
	cleaned.ToolboxRemoved = true
	if !validOperationCompletionForKind(OperationProjectToolboxCleanup, cleaned, "") {
		t.Fatal("toolbox cleanup result was rejected")
	}
}
