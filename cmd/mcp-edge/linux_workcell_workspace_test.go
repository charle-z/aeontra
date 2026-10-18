package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceCLILinuxWorkcellAddAndConfigure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	state := filepath.Join(home, ".local", "state", "mcp-edge")
	projectRoot := filepath.Join(home, "workspaces")
	machineRoot := filepath.Join(home, "htb-machines")
	if err := os.MkdirAll(projectRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(machineRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(projectRoot, "example")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"workspace", "add", "--state", state, "--profile", "linux-workcell", project}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("add code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	fields := strings.Fields(stdout.String())
	if len(fields) < 5 || fields[0] != "added" || fields[2] != "linux-workcell" || fields[3] != "dev" {
		t.Fatalf("add output=%q", stdout.String())
	}
	id := fields[1]
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"workspace", "configure", "--state", state, "--id", id, "--mode", "dev"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("configure code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), id+" linux-workcell dev") {
		t.Fatalf("configure output=%q", stdout.String())
	}
}

func TestHelpDocumentsLinuxWorkcellWorkspaceCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("help code=%d stderr=%q", code, stderr.String())
	}
	for _, command := range []string{"workspace add", "workspace configure", "workspace list", "workspace remove"} {
		if !strings.Contains(stdout.String(), command) {
			t.Fatalf("help missing %q: %s", command, stdout.String())
		}
	}
}
