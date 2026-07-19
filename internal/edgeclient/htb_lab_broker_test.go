//go:build !windows

package edgeclient

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newHTBLabBrokerFixture(t *testing.T) (*htbLabBroker, string) {
	t.Helper()
	registry, _, htbRoot := newLinuxWorkcellRegistry(t)
	workspacePath := filepath.Join(htbRoot, "Cap")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := registry.AddProfile(workspacePath, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = registry.Configure(workspace.ID, WorkspaceConfiguration{
		Mode: WorkspaceModeHTBLinux, MachineName: "Cap", TargetIP: "10.129.63.65",
		Difficulty: "EASY", OS: "LINUX", VPNInterface: "tun0",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"loot", "scans", "reports", "tmp"} {
		if err := os.Mkdir(filepath.Join(workspacePath, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stateRoot := t.TempDir()
	if err := os.Chmod(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	broker := &htbLabBroker{config: HTBLabBrokerConfig{
		StateRoot: stateRoot, Workspace: workspace,
		RuntimeID: "mr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		ToolPath:  openCodeDefaultToolPath,
		Probe:     fakeLinuxNetworkProbe{ipv4: "10.10.15.152", routeInterface: "tun0"},
	}, sessions: make(map[string]htbLabSession), now: time.Now}
	return broker, workspacePath
}

func TestHTBLabBrokerUsesLocalCredentialWithoutLeakingItToSSHArguments(t *testing.T) {
	broker, workspace := newHTBLabBrokerFixture(t)
	artifact := filepath.Join(workspace, "loot", "capture.txt")
	const secret = "fixture-lab-password"
	if err := os.WriteFile(artifact, []byte("USER nathan\nPASS "+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldResolve := htbLabResolveSSH
	oldSelf := htbLabSelfExecutable
	oldRun := htbLabRunSSHProcess
	htbLabResolveSSH = func(name, _ string) (string, bool) {
		if name != "ssh" {
			t.Fatalf("tool=%q", name)
		}
		return "/usr/bin/ssh", true
	}
	htbLabSelfExecutable = func() (string, error) { return "/usr/local/bin/mcp-edge", nil }
	htbLabRunSSHProcess = func(_ context.Context, executable string, args, environment []string, stdin []byte, stdout, stderr io.Writer) error {
		if executable != "/usr/bin/ssh" {
			t.Fatalf("executable=%q", executable)
		}
		joinedArgs := strings.Join(args, " ")
		joinedEnv := strings.Join(environment, "\n")
		if !strings.Contains(joinedArgs, "nathan@10.129.63.65") || strings.Contains(joinedArgs, secret) || strings.Contains(joinedEnv, secret) {
			t.Fatalf("args=%q env=%q", args, environment)
		}
		if len(stdin) != 0 {
			t.Fatalf("stdin=%q", stdin)
		}
		askpassPath := ""
		for _, entry := range environment {
			if strings.HasPrefix(entry, "MCP_DEVBOX_ASKPASS_FILE=") {
				askpassPath = strings.TrimPrefix(entry, "MCP_DEVBOX_ASKPASS_FILE=")
			}
		}
		body, err := os.ReadFile(askpassPath)
		if err != nil || string(body) != secret {
			t.Fatalf("askpass body=%q err=%v", body, err)
		}
		_, _ = io.WriteString(stdout, "uid=1000(nathan) gid=1000(nathan)\n")
		_, _ = io.WriteString(stderr, "Warning: lab fixture\n")
		return nil
	}
	t.Cleanup(func() {
		htbLabResolveSSH = oldResolve
		htbLabSelfExecutable = oldSelf
		htbLabRunSSHProcess = oldRun
	})

	response, err := broker.executeSSH(context.Background(), HTBLabSSHRequest{
		Username: "nathan", Source: "loot/capture.txt", ExtractAfter: "PASS",
		Command: "id", TimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Target != "10.129.63.65" || response.Username != "nathan" || !strings.Contains(response.Stdout, "uid=1000") {
		t.Fatalf("response=%+v", response)
	}
	entries, err := os.ReadDir(filepath.Join(broker.config.StateRoot, "lab-secrets"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("secret files=%v err=%v", entries, err)
	}
}

func TestHTBLabBrokerSavesSensitiveOutputLocally(t *testing.T) {
	broker, workspace := newHTBLabBrokerFixture(t)
	if err := os.WriteFile(filepath.Join(workspace, "loot", "capture.txt"), []byte("PASS fixture-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldResolve := htbLabResolveSSH
	oldSelf := htbLabSelfExecutable
	oldRun := htbLabRunSSHProcess
	htbLabResolveSSH = func(string, string) (string, bool) { return "/usr/bin/ssh", true }
	htbLabSelfExecutable = func() (string, error) { return "/usr/local/bin/mcp-edge", nil }
	htbLabRunSSHProcess = func(_ context.Context, _ string, _ []string, _ []string, _ []byte, stdout, _ io.Writer) error {
		_, _ = io.WriteString(stdout, "0123456789abcdef0123456789abcdef\n")
		return nil
	}
	t.Cleanup(func() {
		htbLabResolveSSH = oldResolve
		htbLabSelfExecutable = oldSelf
		htbLabRunSSHProcess = oldRun
	})
	response, err := broker.executeSSH(context.Background(), HTBLabSSHRequest{
		Username: "nathan", Source: "loot/capture.txt", ExtractAfter: "PASS",
		Command: "cat /home/nathan/user.txt", SaveOutput: "loot/user.txt", TimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Stdout != "" || response.SavedPath != "loot/user.txt" || response.Bytes != 33 || len(response.SHA256) != 64 {
		t.Fatalf("response=%+v", response)
	}
	body, err := os.ReadFile(filepath.Join(workspace, "loot", "user.txt"))
	if err != nil || string(body) != "0123456789abcdef0123456789abcdef\n" {
		t.Fatalf("saved=%q err=%v", body, err)
	}
}

func TestHTBLabBrokerRejectsTargetAndAmbiguousCredentialFields(t *testing.T) {
	request := `{"username":"nathan","source":"loot/capture.txt","extract_after":"PASS","command":"id","target":"10.129.59.198"}`
	if _, err := decodeHTBLabBrokerRequest(bytes.NewBufferString(request)); err == nil {
		t.Fatal("caller-selected target accepted")
	}
	broker, workspace := newHTBLabBrokerFixture(t)
	if err := os.WriteFile(filepath.Join(workspace, "loot", "capture.txt"), []byte("PASS first\nPASS second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	validated, err := validateHTBLabSSHRequest(HTBLabSSHRequest{
		Username: "nathan", Source: "loot/capture.txt", ExtractAfter: "PASS", Command: "id", TimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := extractHTBLabCredential(broker.config.Workspace.Path, validated); err == nil {
		t.Fatal("ambiguous credential accepted")
	}
}

func TestBuildHTBLabSSHArgsUsesOnlyRegisteredTarget(t *testing.T) {
	request := HTBLabSSHRequest{Username: "nathan", Command: "id && cat /home/nathan/user.txt"}
	args := buildHTBLabSSHArgs(request, "10.129.63.65")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "nathan@10.129.63.65") || !strings.Contains(joined, request.Command) {
		t.Fatalf("args=%q", args)
	}
	for _, forbidden := range []string{"fixture-lab-password", "10.129.59.198", "sshpass"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("args=%q", args)
		}
	}
}

func TestHTBLabCredentialPrefixIsLiteralAndDelimited(t *testing.T) {
	broker, workspace := newHTBLabBrokerFixture(t)
	path := filepath.Join(workspace, "loot", "capture.txt")
	if err := os.WriteFile(path, []byte("PASSWORD wrong-value\nPASS correct-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := validateHTBLabSSHRequest(HTBLabSSHRequest{
		Username: "nathan", Source: "loot/capture.txt", ExtractAfter: "PASS", Command: "id", TimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := extractHTBLabCredential(broker.config.Workspace.Path, request)
	if err != nil || string(value) != "correct-value" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	zeroHTBBytes(value)
}

func TestHTBLabOutputRejectsSymlinkTarget(t *testing.T) {
	_, workspace := newHTBLabBrokerFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("preserve\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspace, "loot", "user.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := writeHTBLabOutput(workspace, "loot/user.txt", []byte("replace\n")); err == nil {
		t.Fatal("symlink output target accepted")
	}
	body, err := os.ReadFile(outside)
	if err != nil || string(body) != "preserve\n" {
		t.Fatalf("outside=%q err=%v", body, err)
	}
}

func TestHTBLabArtifactRejectsSymlinkSource(t *testing.T) {
	broker, workspace := newHTBLabBrokerFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("PASS outside-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "loot", "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	request, err := validateHTBLabSSHRequest(HTBLabSSHRequest{
		Username: "nathan", Source: "loot/link.txt", ExtractAfter: "PASS", Command: "id", TimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := extractHTBLabCredential(broker.config.Workspace.Path, request); err == nil {
		t.Fatal("symlink credential source accepted")
	}
}
