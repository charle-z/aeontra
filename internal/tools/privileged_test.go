package tools

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

func TestPrivilegedTasksDisabledByDefault(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	if _, err := svc.PrivilegedTaskPreview("", "go-test", nil); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("feature must be disabled by default: %v", err)
	}
}

func TestPrivilegedPreviewShowsExactServerCommandAndPlan(t *testing.T) {
	var log bytes.Buffer
	svc, root := newTestServiceWithAudit(t, config.ModeAllow, &log)
	svc.WithPrivilegedConfig(PrivilegedConfig{Enabled: true, AllowedServices: []string{"mcp-devbox"}})
	out, err := svc.PrivilegedTaskPreview(root, "go-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Command:\ngo test ./... -count=1", "Working directory:\n" + root,
		"Network:\ndisabled", "Filesystem access:\n" + root,
		"Expected effect:", "Risk:", "Plan ID:", "Expiry:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(log.String(), "privileged_task_preview") {
		t.Fatalf("preview not audited: %s", log.String())
	}
}

func TestPrivilegedProfilesRejectInvalidParametersTraversalAndServices(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	svc.WithPrivilegedConfig(PrivilegedConfig{Enabled: true, AllowedServices: []string{"mcp-devbox"}})
	for _, tc := range []struct {
		repo    string
		profile string
		params  map[string]string
	}{
		{"", "unknown", nil},
		{"../escape", "go-test", nil},
		{root, "go-test", map[string]string{"command": "sh -c id"}},
		{root, "git-fetch", map[string]string{"remote": "origin;whoami"}},
		{root, "git-fast-forward", map[string]string{"branch": "main$(id)"}},
		{"", "restart-approved-service", map[string]string{"service": "ssh"}},
		{"", "inspect-approved-service-status", map[string]string{"service": "mcp-devbox;id"}},
	} {
		if _, err := svc.PrivilegedTaskPreview(tc.repo, tc.profile, tc.params); err == nil {
			t.Fatalf("unsafe profile request accepted: %#v", tc)
		}
	}
}

func TestPrivilegedExecuteApprovalReplayExpiryAndTimeout(t *testing.T) {
	svc, root := newTestService(t, config.ModeAsk)
	svc.WithPrivilegedConfig(PrivilegedConfig{Enabled: true, Timeout: 20 * time.Millisecond})
	var calls int
	svc.WithRunner(func(ctx context.Context, _ string, _ string, _ []string) (string, error) {
		calls++
		<-ctx.Done()
		return "", ctx.Err()
	})
	preview, err := svc.PrivilegedTaskPreview(root, "go-vet", nil)
	if err != nil {
		t.Fatal(err)
	}
	planID := privilegedField(preview, "Plan ID")
	out, err := svc.PrivilegedTaskExecute(planID, false)
	if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") || calls != 0 {
		t.Fatalf("approval gate: out=%q err=%v calls=%d", out, err, calls)
	}
	if _, err := svc.PrivilegedTaskExecute(planID, true); err == nil || !strings.Contains(err.Error(), "deadline") || calls != 1 {
		t.Fatalf("timeout not enforced: err=%v calls=%d", err, calls)
	}
	if _, err := svc.PrivilegedTaskExecute(planID, true); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("replay must fail: %v", err)
	}

	preview, _ = svc.PrivilegedTaskPreview(root, "go-build", nil)
	expired := privilegedField(preview, "Plan ID")
	svc.plans.mu.Lock()
	svc.plans.plans[expired].ExpiresAt = time.Now().Add(-time.Minute)
	svc.plans.mu.Unlock()
	if _, err := svc.PrivilegedTaskExecute(expired, true); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired plan must fail: %v", err)
	}
}

func TestPrivilegedDockerProfileFailsSecurelyWithoutContainment(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	svc.WithPrivilegedConfig(PrivilegedConfig{Enabled: true})
	preview, err := svc.PrivilegedTaskPreview(root, "docker-build-project", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrivilegedTaskExecute(privilegedField(preview, "Plan ID"), true); err == nil || !strings.Contains(err.Error(), "Docker socket") {
		t.Fatalf("docker profile must fail securely: %v", err)
	}
}

func TestPrivilegedServiceProfileUsesExactAllowlistedCommand(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithPrivilegedConfig(PrivilegedConfig{Enabled: true, AllowedServices: []string{"mcp-devbox"}})
	var gotProg string
	var gotArgs []string
	svc.WithRunner(func(_ context.Context, _ string, prog string, args []string) (string, error) {
		gotProg, gotArgs = prog, append([]string(nil), args...)
		return "ok", nil
	})
	preview, err := svc.PrivilegedTaskPreview("", "restart-approved-service", map[string]string{"service": "mcp-devbox"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrivilegedTaskExecute(privilegedField(preview, "Plan ID"), true); err != nil {
		t.Fatal(err)
	}
	if gotProg != "systemctl" || strings.Join(gotArgs, " ") != "restart -- mcp-devbox" {
		t.Fatalf("command = %s %#v", gotProg, gotArgs)
	}
}

func newTestServiceWithAudit(t *testing.T, mode config.Mode, out *bytes.Buffer) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	cfg, err := config.New(config.Config{Roots: []string{root}, Mode: mode, AllowedCommands: []string{"git", "go", "gofmt"}})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(pol, audit.New(out), root), root
}

func privilegedField(text, key string) string {
	lines := strings.Split(text, "\n")
	for i := 0; i+1 < len(lines); i++ {
		if lines[i] == key+":" {
			return strings.TrimSpace(lines[i+1])
		}
	}
	return ""
}
