package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestMemoryRead_EmptyAndPopulated(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	out, err := svc.MemoryRead()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no agent memory") {
		t.Errorf("expected empty-memory note, got %q", out)
	}
	write(t, root, ".agent-memory/conventions.md", "# Conventions\nUse TDD.\n")
	out, err = svc.MemoryRead()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Use TDD") {
		t.Errorf("memory content missing: %q", out)
	}
}

func TestMemoryRead_RedactsSecrets(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	write(t, root, ".agent-memory/notes.md", "token gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz\n")
	out, err := svc.MemoryRead()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("memory leaked a secret: %q", out)
	}
}

func TestMemoryUpdateHandoff_ReadOnlyDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	if _, err := svc.MemoryUpdateHandoff("did stuff"); err == nil {
		t.Error("handoff write in read-only mode should be denied")
	}
}

func TestMemoryUpdateHandoff_WritesAndRedacts(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	_, err := svc.MemoryUpdateHandoff("finished step; api_key=supersecretvalue123 should not persist")
	if err != nil {
		t.Fatal(err)
	}
	latest := filepath.Join(root, ".agent-memory", "handoffs", "latest.md")
	data, err := os.ReadFile(latest)
	if err != nil {
		t.Fatalf("latest.md not written: %v", err)
	}
	if strings.Contains(string(data), "supersecretvalue123") {
		t.Errorf("secret persisted into handoff: %q", data)
	}
	// And it is readable back via memory_read.
	out, _ := svc.MemoryRead()
	if !strings.Contains(out, "Handoff") {
		t.Errorf("handoff not surfaced by memory_read: %q", out)
	}
}

func TestMemoryWrite_ReadOnlyDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	if _, err := svc.MemoryWrite("plan", "next step", false); err == nil {
		t.Error("memory_write in read-only mode should be denied")
	}
}

func TestMemoryWrite_AskRequiresApproval(t *testing.T) {
	svc, root := newTestService(t, config.ModeAsk)
	out, err := svc.MemoryWrite("plan", "run the focused tests", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "APPROVAL REQUIRED") {
		t.Fatalf("ask mode should require approval, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-memory", "plan.md")); !os.IsNotExist(err) {
		t.Fatalf("memory_write should not write before approval, stat err=%v", err)
	}
	if _, err := svc.MemoryWrite("plan", "run the focused tests", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-memory", "plan.md")); err != nil {
		t.Fatalf("approved memory_write did not write plan.md: %v", err)
	}
}

func TestMemoryWrite_WritesAllowedSectionAndRedacts(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	_, err := svc.MemoryWrite("current-task", "Ship P1-5 with api_key=supersecretvalue123", false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".agent-memory", "current-task.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "supersecretvalue123") {
		t.Fatalf("secret persisted into structured memory: %q", data)
	}
	if !strings.Contains(string(data), "***REDACTED-SECRET***") {
		t.Fatalf("structured memory should contain redacted marker: %q", data)
	}
	out, err := svc.MemoryRead()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "current-task.md") || !strings.Contains(out, "Ship P1-5") {
		t.Fatalf("memory_read should surface structured memory, got %q", out)
	}
}

func TestMemoryWrite_RejectsUnknownSection(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	for _, section := range []string{"handoffs/latest", "../plan", "notes", "plan.md"} {
		if _, err := svc.MemoryWrite(section, "content", false); err == nil {
			t.Fatalf("memory_write should reject section %q", section)
		}
	}
}

func TestBuildContextPack_AssemblesAndRedacts(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	write(t, root, "README.md", "# My Project\nleaky gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz\n")
	write(t, root, "src/app.go", "package src\n")
	write(t, root, ".env", "SECRET=should_not_appear_value")
	write(t, root, ".agent-memory/state.md", "current task: build L1\n")

	out, err := svc.BuildContextPack()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "src/app.go") {
		t.Errorf("file tree missing source file: %q", out)
	}
	if !strings.Contains(out, "My Project") {
		t.Errorf("key file README missing: %q", out)
	}
	if !strings.Contains(out, "build L1") {
		t.Errorf("memory missing from pack: %q", out)
	}
	if strings.Contains(out, "gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("secret leaked into context pack: %q", out)
	}
	if strings.Contains(out, "should_not_appear_value") {
		t.Errorf(".env content leaked into context pack: %q", out)
	}
}
