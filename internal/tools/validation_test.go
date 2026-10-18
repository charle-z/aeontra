package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

type fakeValidationRunner struct {
	available     bool
	repo, profile string
	result        ValidationResult
	err           error
}

func (f *fakeValidationRunner) Available() bool { return f.available }
func (f *fakeValidationRunner) Run(_ context.Context, repo, profile string) (ValidationResult, error) {
	f.repo, f.profile = repo, profile
	return f.result, f.err
}

func TestValidationPreviewAndExecuteUsesFixedProfile(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	write(t, root, "portfolio/package.json", "{}")
	r := &fakeValidationRunner{available: true, result: ValidationResult{Output: "build okay"}}
	svc.WithValidationRunner(r)
	preview, err := svc.ValidationPreview("portfolio", "pnpm-validate")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, "Plan ID:") || !strings.Contains(preview, "Network: disabled") {
		t.Fatalf("unexpected preview: %s", preview)
	}
	planID := strings.TrimSpace(strings.Split(strings.Split(preview, "Plan ID:")[1], "\n")[0])
	out, err := svc.ValidationExecute(planID, true)
	if err != nil || out != "build okay" {
		t.Fatalf("execute = %q, %v", out, err)
	}
	if r.repo != "portfolio" || r.profile != "pnpm-validate" {
		t.Fatalf("runner got repo=%q profile=%q", r.repo, r.profile)
	}
}

func TestValidationPreviewRejectsNestedRepoAndUnknownProfile(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	write(t, root, "portfolio/package.json", "{}")
	svc.WithValidationRunner(&fakeValidationRunner{available: true})
	if _, err := svc.ValidationPreview("portfolio/nested", "pnpm-validate"); err == nil {
		t.Fatal("nested repo accepted")
	}
	if _, err := svc.ValidationPreview("portfolio", "anything"); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestValidationRunnerUnavailableFailsSecurely(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	write(t, root, "portfolio/package.json", "{}")
	if _, err := svc.ValidationPreview("portfolio", "pnpm-validate"); err == nil {
		t.Fatal("unconfigured runner accepted")
	}
}
