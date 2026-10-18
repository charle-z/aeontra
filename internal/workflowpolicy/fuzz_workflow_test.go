package workflowpolicy

import (
	"os"
	"strings"
	"testing"
)

func TestScheduledFuzzWorkflowCoversEveryKnownTarget(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/fuzz.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)

	for _, required := range []string{
		"schedule:",
		"cron: \"17 3 * * 1\"",
		"workflow_dispatch:",
		"contents: read",
		"timeout-minutes: 10",
		"GOMAXPROCS: \"2\"",
		"go test ${{ matrix.package }} -run=NoTests -fuzz=${{ matrix.target }} -fuzztime=30s",
		"FuzzJailResolveNeverReturnsOutsideRoot",
		"FuzzCheckCommandAllowedResultSatisfiesAllGates",
		"FuzzRedactIsIdempotent",
		"FuzzAccessGrantTTLBoundary",
		"FuzzJSONRPCHandleReturnsValidJSONOrNotification",
		"FuzzJSONRPCBatchResponsesStayBoundedAndValid",
		"FuzzActionPlanSingleUseAndOperationBinding",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("fuzz.yml does not contain %q", required)
		}
	}
	if strings.Contains(text, "pull_request:") || strings.Contains(text, "push:") {
		t.Error("timed fuzzing must remain scheduled/manual, not PR or push blocking")
	}
	if strings.Contains(text, "secrets.") || strings.Contains(text, "continue-on-error: true") {
		t.Error("fuzz workflow must not use secrets or suppress failures")
	}
	if got := strings.Count(text, "target:"); got != 7 {
		t.Fatalf("matrix target count = %d, want 7", got)
	}
}

func TestFuzzWorkflowTargetSetMatchesGoFuzzFunctions(t *testing.T) {
	workflow, err := os.ReadFile("../../.github/workflows/fuzz.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflowText := string(workflow)
	files := []string{
		"../policy/fuzz_test.go",
		"../mcpserver/fuzz_test.go",
		"../tools/action_plan_fuzz_test.go",
	}
	seen := 0
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(content), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "func Fuzz") {
				continue
			}
			name := strings.TrimPrefix(strings.SplitN(line, "(", 2)[0], "func ")
			if !strings.Contains(workflowText, "target: "+name) {
				t.Errorf("fuzz target %s missing from workflow", name)
			}
			seen++
		}
	}
	if seen != 7 {
		t.Fatalf("discovered fuzz targets = %d, want 7", seen)
	}
}
