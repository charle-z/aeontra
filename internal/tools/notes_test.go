package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestNotesCreateListReadAppendAndRedact(t *testing.T) {
	svc, root := newTestService(t, config.ModeAsk)
	secret := "ghp_0123456789abcdefghijklmnopqrstuvwxyz"
	preview, err := svc.NotesWritePreview("ideas", "# Ideas\n\ntoken "+secret, "create")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(preview, secret) {
		t.Fatalf("preview leaked secret: %s", preview)
	}
	planID := field(preview, "plan_id")
	out, err := svc.NotesWrite(planID, false)
	if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") {
		t.Fatalf("approval gate: out=%q err=%v", out, err)
	}
	if _, err := svc.NotesWrite(planID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NotesWrite(planID, true); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("replay must fail: %v", err)
	}

	list, err := svc.NotesList()
	if err != nil || !strings.Contains(list, "name: ideas") || !strings.Contains(list, "size:") || !strings.Contains(list, "updated:") {
		t.Fatalf("bad list: %q err=%v", list, err)
	}
	read, err := svc.NotesRead("ideas")
	if err != nil || !strings.Contains(read, "# Ideas") || strings.Contains(read, secret) {
		t.Fatalf("bad read: %q err=%v", read, err)
	}

	preview, err = svc.NotesWritePreview("ideas", "second entry", "append")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NotesWrite(field(preview, "plan_id"), true); err != nil {
		t.Fatal(err)
	}
	read, _ = svc.NotesRead("ideas")
	if !strings.Contains(read, "second entry") || !strings.Contains(read, "appended:") {
		t.Fatalf("append missing timestamp/content: %s", read)
	}
	if _, err := os.Stat(filepath.Join(root, notesDir, "ideas.md")); err != nil {
		t.Fatal(err)
	}
}

func TestNotesRejectTraversalInvalidSlugOverwriteAndSize(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	for _, name := range []string{"../evil", "a/b", ".hidden", "note.md", "UPPER", "--flag", ""} {
		if _, err := svc.NotesWritePreview(name, "content", "create"); err == nil {
			t.Fatalf("invalid note name accepted: %q", name)
		}
	}
	if _, err := svc.NotesWritePreview("valid", strings.Repeat("x", maxNoteBytes+1), "create"); err == nil {
		t.Fatal("oversized note accepted")
	}
	preview, _ := svc.NotesWritePreview("valid", "one", "create")
	if _, err := svc.NotesWrite(field(preview, "plan_id"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NotesWritePreview("valid", "replacement", "create"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("overwrite must be refused: %v", err)
	}
	if _, err := svc.NotesWritePreview("missing", "append", "append"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("append to missing note must fail: %v", err)
	}
}

func TestNotesRejectSymlinkEscape(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	outside := t.TempDir()
	base := filepath.Join(root, notesDir)
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, base); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := svc.NotesWritePreview("escape", "bad", "create"); err == nil {
		t.Fatal("notes directory symlink escape accepted")
	}
}

func TestNotesRejectChangedAppendState(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	preview, _ := svc.NotesWritePreview("stable", "base", "create")
	_, _ = svc.NotesWrite(field(preview, "plan_id"), true)
	preview, _ = svc.NotesWritePreview("stable", "append", "append")
	if err := os.WriteFile(filepath.Join(root, notesDir, "stable.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NotesWrite(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed append target must fail: %v", err)
	}
}

func TestNotesExpiredPlanAndReadOnlyWrite(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	preview, _ := svc.NotesWritePreview("expiry", "content", "create")
	planID := field(preview, "plan_id")
	svc.plans.mu.Lock()
	svc.plans.plans[planID].ExpiresAt = time.Now().Add(-time.Minute)
	svc.plans.mu.Unlock()
	if _, err := svc.NotesWrite(planID, true); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired note plan must fail: %v", err)
	}

	ro, _ := newTestService(t, config.ModeReadOnly)
	preview, _ = ro.NotesWritePreview("readonly", "content", "create")
	if _, err := ro.NotesWrite(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("read-only write must fail: %v", err)
	}
}
