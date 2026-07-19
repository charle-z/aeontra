//go:build !windows

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestDefaultStateRootPrefersXDGAndFallsBackToLegacyIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	preferred := filepath.Join(home, "state", "mcp-edge")
	if got := defaultStateRoot(); got != preferred {
		t.Fatalf("preferred state=%q want %q", got, preferred)
	}
	legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "identity.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := defaultStateRoot(); got != legacy {
		t.Fatalf("legacy state=%q want %q", got, legacy)
	}
	if err := os.MkdirAll(preferred, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(preferred, "identity.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := defaultStateRoot(); got != preferred {
		t.Fatalf("preferred identity state=%q want %q", got, preferred)
	}
}

func TestLabInitCreatesAndReusesAuthorizedHTBWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	state := filepath.Join(home, ".local", "state", "mcp-edge")
	oldRoute := labRouteLookup
	labRouteLookup = func(context.Context, string, string) (string, string, error) {
		return "tun0", "10.10.14.9", nil
	}
	t.Cleanup(func() { labRouteLookup = oldRoute })

	args := []string{
		"lab", "init", "--state", state, "--platform", "htb", "--machine", "Cap",
		"--target", "10.129.63.65", "--difficulty", "easy", "--os", "linux",
	}
	var stdout, stderr bytes.Buffer
	if code := run(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("first init code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "lab-ready created ws_") || !strings.Contains(stdout.String(), "target=10.129.63.65 vpn=tun0 lhost=10.10.14.9") {
		t.Fatalf("first init output=%q", stdout.String())
	}
	workspacePath := filepath.Join(home, "htb-machines", "Cap")
	for _, relative := range []string{".git", "README.md", "scans", "loot", "scripts", "reports", "tmp", "tickets"} {
		if _, err := os.Stat(filepath.Join(workspacePath, relative)); err != nil {
			t.Fatalf("missing %s: %v", relative, err)
		}
	}
	registry, err := edgeclient.OpenWorkspaceRegistry(state)
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := registry.List()
	_ = registry.Close()
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("workspaces=%+v err=%v", workspaces, err)
	}
	workspace := workspaces[0]
	if workspace.Mode != edgeclient.WorkspaceModeHTBLinux || workspace.MachineName != "Cap" || workspace.TargetIP != "10.129.63.65" || workspace.VPNInterface != "tun0" {
		t.Fatalf("workspace=%+v", workspace)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("second init code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "lab-ready existing "+workspace.ID) {
		t.Fatalf("second init output=%q", stdout.String())
	}
}

func TestLabInitRejectsUnsafeMachineBeforeCreatingWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldRoute := labRouteLookup
	labRouteLookup = func(context.Context, string, string) (string, string, error) {
		t.Fatal("route lookup must not run for an invalid machine")
		return "", "", nil
	}
	t.Cleanup(func() { labRouteLookup = oldRoute })
	var stdout, stderr bytes.Buffer
	code := run([]string{"lab", "init", "--machine", "../escape", "--target", "10.10.10.10", "--difficulty", "easy"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "machine name is invalid") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, "escape")); !os.IsNotExist(err) {
		t.Fatalf("unsafe workspace created: %v", err)
	}
}

func TestHelpDocumentsSingleCommandLabOnboarding(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	for _, expected := range []string{"lab init", "lab ssh-exec"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("help missing %q: %s", expected, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "--password") || strings.Contains(stdout.String(), "--target <IP> --username") {
		t.Fatalf("help exposes caller-supplied credential or SSH target: %s", stdout.String())
	}
}

func TestLabInitFailsBeforeWorkspaceCreationWhenVPNRouteIsInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldRoute := labRouteLookup
	labRouteLookup = func(context.Context, string, string) (string, string, error) {
		return "", "", errors.New("route unavailable")
	}
	t.Cleanup(func() { labRouteLookup = oldRoute })
	var stdout, stderr bytes.Buffer
	code := run([]string{"lab", "init", "--machine", "Cap", "--target", "10.129.63.65", "--difficulty", "easy"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "route unavailable") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, "htb-machines", "Cap")); !os.IsNotExist(err) {
		t.Fatalf("workspace created before VPN validation: %v", err)
	}
}
