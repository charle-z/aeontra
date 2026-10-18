package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestDevelopmentPlannerChoosesLocalCommandsFromProjectMarkers(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stages, err := PlanValidation(root)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"go", "test", "./..."}, {"go", "vet", "./..."}, {"go", "build", "./..."}}
	if len(stages) != len(want) {
		t.Fatalf("stages=%+v", stages)
	}
	for index := range want {
		if strings.Join(stages[index].Argv, " ") != strings.Join(want[index], " ") {
			t.Fatalf("stage %d=%v", index, stages[index].Argv)
		}
	}
}

func TestDevelopmentPlannerUsesOnlyDeclaredPackageScripts(t *testing.T) {
	root := t.TempDir()
	packageJSON := `{"scripts":{"check":"astro check","test":"vitest run","build":"astro build","postinstall":"curl bad"}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(packageJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stages, err := PlanValidation(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, stage := range stages {
		joined += strings.Join(stage.Argv, " ") + "\n"
	}
	if strings.Contains(joined, "postinstall") || joined != "pnpm run check\npnpm test\npnpm run build\n" {
		t.Fatalf("stages=%q", joined)
	}
}

func TestWorkspaceRejectsRootMountedWindowsAndSymlinkEscape(t *testing.T) {
	for _, root := range []string{"/", "/mnt/c/work", "/mnt/d/project", "relative"} {
		if _, err := ResolveWorkspace(root, "project"); err == nil {
			t.Fatalf("root %q accepted", root)
		}
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "project")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ResolveWorkspace(root, "project"); err == nil {
		t.Fatal("symlink workspace accepted")
	}
}

func TestWorkspaceRejectsGroupWritableDirectory(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "project")
	if err := os.Mkdir(workspace, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(workspace, 0o770); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWorkspace(root, "project"); err == nil {
		t.Fatal("group-writable workspace accepted")
	}
}

func TestBubblewrapPlanHasNoNetworkOrHostHomeAndNoRemoteCommand(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	stage := Stage{Name: "go_test", Argv: []string{"go", "test", "./..."}}
	args, err := BubblewrapArgs(workspace, stage, edge.NetworkNone)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, forbidden := range []string{"/mnt/c", "/mnt/d", "/var/run/docker.sock", "--share-net", "sudo", "sh -c"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("bubblewrap args contain %q: %s", forbidden, joined)
		}
	}
	if !strings.Contains(joined, "--unshare-all") || !strings.Contains(joined, "--bind "+workspace+" /workspace") || !strings.HasSuffix(joined, "-- go test ./...") {
		t.Fatalf("args=%s", joined)
	}
	if _, err := BubblewrapArgs(workspace, stage, edge.NetworkRegistry); err == nil {
		t.Fatal("unenforced registry network policy accepted")
	}
}

func TestWorkcellBoundsAndRedactsFailureOutput(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "project")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeCommandRunner{exitCode: 1, output: []byte("Authorization: Bearer secret-token-value\nfailed")}
	workcell := &Workcell{Root: root, Commands: runner}
	result := workcell.Execute(context.Background(), edge.Task{
		Objective:    edge.Objective{Kind: edge.ObjectiveValidate, Summary: "validate"},
		Restrictions: edge.Restrictions{Workspace: "project", NetworkPolicy: edge.NetworkNone, MaxDurationSeconds: 60, MaxOutputBytes: 1024},
	})
	if result.Outcome != edge.OutcomeFailed || strings.Contains(result.Summary, "secret-token-value") || len(result.Summary) > 2048 {
		t.Fatalf("result=%+v", result)
	}
	if runner.calls != 1 {
		t.Fatalf("calls=%d", runner.calls)
	}
}

func TestBoundedSummaryPreservesUTF8AndByteLimit(t *testing.T) {
	value := boundedSummary(strings.Repeat("€", 1000))
	if !utf8.ValidString(value) || len(value) > 2000 {
		t.Fatalf("valid=%v bytes=%d", utf8.ValidString(value), len(value))
	}
}

type fakeCommandRunner struct {
	calls    int
	exitCode int
	output   []byte
}

func (f *fakeCommandRunner) Run(_ context.Context, _ string, _ []string, _ int64) (int, []byte, error) {
	f.calls++
	return f.exitCode, f.output, nil
}
