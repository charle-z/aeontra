//go:build !windows

package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestRunDirectWorkcellCommandRedactsAndBoundsOutput(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := "ghp_" + strings.Repeat("A", 36)
	runner := &fakeDirectWorkcellRunner{
		customOutput: true,
		stdout:       "token=" + secret + "\n" + strings.Repeat("x", edge.MaxProjectExecStreamBytes+1024),
		stderr:       "credential=" + secret + "\n",
	}
	result, err := RunDirectWorkcellCommand(context.Background(), DirectWorkcellCommandRequest{
		OperationID: "eo_0123456789abcdef0123456789abcdef",
		Workspace: Workspace{
			ID: "ws_0123456789abcdef0123456789abcdef", Path: workspacePath,
			Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev,
		},
		Argv: []string{"printf", "safe"}, TimeoutSeconds: 10,
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Stdout+result.Stderr, secret) || !strings.Contains(result.Stdout, "REDACTED") || !strings.Contains(result.Stderr, "REDACTED") {
		t.Fatalf("secret was not redacted: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
	if len(result.Stdout) > edge.MaxProjectExecStreamBytes || len(result.Stderr) > edge.MaxProjectExecStreamBytes {
		t.Fatalf("unbounded output: stdout=%d stderr=%d", len(result.Stdout), len(result.Stderr))
	}
	if !result.StdoutTruncated || result.StderrTruncated {
		t.Fatalf("truncation flags are wrong: %+v", result)
	}
}
