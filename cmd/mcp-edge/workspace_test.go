package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceCLIAddListRemove(t *testing.T) {
	state := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"workspace", "add", "--state", state, "--path", workspace}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("add code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	fields := strings.Fields(stdout.String())
	if len(fields) != 3 || fields[0] != "added" || !strings.HasPrefix(fields[1], "ws_") || fields[2] != workspace {
		t.Fatalf("add output=%q", stdout.String())
	}
	workspaceID := fields[1]
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"workspace", "list", "--state", state}, strings.NewReader(""), &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), workspaceID+" "+workspace) {
		t.Fatalf("list code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"workspace", "remove", "--state", state, "--id", workspaceID}, strings.NewReader(""), &stdout, &stderr); code != 0 || strings.TrimSpace(stdout.String()) != "removed "+workspaceID {
		t.Fatalf("remove code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestWorkspaceCLIHasNoRemotePathOrCallerSelectedID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"workspace", "add", "--id", "ws_0123456789abcdef0123456789abcdef", "--path", "/tmp/project"}, strings.NewReader(""), &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("caller-selected id accepted: code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"workspace", "add", "--path", "/mnt/c/project"}, strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatalf("Windows mount accepted: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestHelpDocumentsOnlyLocalWorkspaceManagement(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("help code=%d stderr=%q", code, stderr.String())
	}
	text := stdout.String()
	for _, command := range []string{"workspace add", "workspace list", "workspace remove"} {
		if !strings.Contains(text, command) {
			t.Fatalf("help missing %q: %s", command, text)
		}
	}
	for _, forbidden := range []string{"workspace serve", "workspace remote", "--host-path"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("help exposes forbidden workspace operation %q", forbidden)
		}
	}
}
