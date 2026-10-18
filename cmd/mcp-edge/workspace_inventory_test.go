package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestWorkspaceInventoryReturnsSanitizedLocalToolMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	state := filepath.Join(home, ".local", "state", "mcp-edge")
	projectRoot := filepath.Join(home, "workspaces")
	if err := os.MkdirAll(projectRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(projectRoot, "inventory-project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"workspace", "add", "--state", state, "--profile", "linux-workcell", project}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("add code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	fields := strings.Fields(stdout.String())
	if len(fields) < 2 {
		t.Fatalf("add output=%q", stdout.String())
	}
	workspaceID := fields[1]
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"workspace", "inventory", "--state", state, workspaceID}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("inventory code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var response struct {
		WorkspaceID string                               `json:"workspace_id"`
		Profile     edgeclient.WorkspaceProfile          `json:"profile"`
		Mode        edgeclient.WorkspaceMode             `json:"mode"`
		Tools       []edgeclient.LinuxToolInventoryEntry `json:"tools"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.WorkspaceID != workspaceID || response.Profile != edgeclient.WorkspaceProfileLinuxWorkcell || response.Mode != edgeclient.WorkspaceModeDev || len(response.Tools) < 20 {
		t.Fatalf("response=%+v", response)
	}
	if strings.Contains(stdout.String(), project) || strings.Contains(stdout.String(), home) {
		t.Fatalf("inventory leaked a local path: %s", stdout.String())
	}
	if err := edgeclient.ValidateLinuxToolInventory(response.Tools); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceInventoryRejectsSandboxProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	state := filepath.Join(home, ".local", "state", "mcp-edge")
	project := filepath.Join(home, "sandbox-project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"workspace", "add", "--state", state, "--path", project}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("add code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	workspaceID := strings.Fields(stdout.String())[1]
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"workspace", "inventory", "--state", state, workspaceID}, strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatalf("sandbox inventory unexpectedly succeeded: %s", stdout.String())
	}
}
