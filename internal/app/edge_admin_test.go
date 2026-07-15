package app

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestEdgeAdminCreatesOneTimePairingAndRevokesDevice(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"edge", "pairing-create", "--state-root", stateRoot}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	pairingCode := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(pairingCode, "ep_") || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	store, err := edge.Open(edge.Config{Root: filepath.Join(stateRoot, "edge")})
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(pairingCode, "wsl-development", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	stdout.Reset()
	if code := run([]string{"edge", "devices", "--state-root", stateRoot}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), device.ID) {
		t.Fatalf("devices code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	createArgs := []string{
		"edge", "task-create", "--state-root", stateRoot,
		"--device", device.ID,
		"--idempotency", "admin-task-0001",
		"--workspace", "portfolio-charlez",
		"--objective", "validate the project",
		"--accept", "checks pass",
	}
	if code := run(createArgs, &stdout, &stderr); code != 0 {
		t.Fatalf("task create code=%d stderr=%s", code, stderr.String())
	}
	var task edge.Task
	if err := json.Unmarshal(stdout.Bytes(), &task); err != nil || task.ID == "" {
		t.Fatalf("task=%+v err=%v", task, err)
	}
	stdout.Reset()
	if code := run([]string{"edge", "task-status", "--state-root", stateRoot, "--task", task.ID}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), task.ID) {
		t.Fatalf("task status code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := run([]string{"edge", "task-cancel", "--state-root", stateRoot, "--task", task.ID}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), task.ID) {
		t.Fatalf("task cancel code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"edge", "revoke", "--state-root", stateRoot, "--device", device.ID}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "revoked "+device.ID {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestEdgeAdminRejectsMissingOrRelativeStateRoot(t *testing.T) {
	for _, args := range [][]string{
		{"edge"},
		{"edge", "pairing-create"},
		{"edge", "pairing-create", "--state-root", "relative"},
		{"edge", "revoke", "--state-root", t.TempDir()},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code == 0 {
			t.Fatalf("args=%v accepted stdout=%q", args, stdout.String())
		}
	}
}
