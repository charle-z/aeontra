package edge

import (
	"strings"
	"testing"
)

func TestProjectExecRequestRejectsSecretShapedInputs(t *testing.T) {
	secret := "ghp_" + strings.Repeat("A", 36)
	base := OperationRequest{
		Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell",
		IdempotencyKey: "secret-guard", Argv: []string{"printf", "safe"}, TimeoutSeconds: 10,
	}
	cases := []OperationRequest{
		func() OperationRequest { request := base; request.Argv = []string{"printf", secret}; return request }(),
		func() OperationRequest { request := base; request.Stdin = secret; return request }(),
		func() OperationRequest {
			request := base
			request.Environment = map[string]string{"CI": secret}
			return request
		}(),
	}
	for _, request := range cases {
		if _, err := normalizeProjectExecRequest(request); err == nil {
			t.Fatalf("secret-shaped request was accepted: %+v", request)
		}
	}
	if _, err := normalizeProjectExecRequest(base); err != nil {
		t.Fatalf("safe request rejected: %v", err)
	}
}

func TestProjectExecRequestRejectsContainerHelperAuthorityEnvironment(t *testing.T) {
	base := OperationRequest{
		Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell",
		IdempotencyKey: "container-helper-guard", Argv: []string{"true"}, TimeoutSeconds: 10,
	}
	for _, key := range []string{"CONTAINERS_HELPER_BINARY_DIR", "CONTAINERS_CONF", "CONTAINERS_CONF_OVERRIDE", "CONTAINERS_CONF_MODULES", "CONTAINERS_STORAGE_CONF"} {
		request := base
		request.Environment = map[string]string{key: "/workspace/untrusted"}
		if _, err := normalizeProjectExecRequest(request); err == nil {
			t.Fatalf("reserved environment %s was accepted", key)
		}
	}
}
