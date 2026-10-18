package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
)

func TestRunPreservesVersionAndHelpCommands(t *testing.T) {
	oldCommit := buildinfo.Commit
	buildinfo.Commit = "test-commit"
	defer func() { buildinfo.Commit = oldCommit }()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("version exit code = %d", code)
	}
	if got := stdout.String(); !strings.Contains(got, "mcp-devbox "+buildinfo.Version+" (commit test-commit)") {
		t.Fatalf("version output = %q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("version stderr = %q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") || stdout.Len() != 0 {
		t.Fatalf("help stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunPreservesUsageAndErrorExitCodes(t *testing.T) {
	for name, testCase := range map[string]struct {
		args     []string
		wantCode int
		wantText string
	}{
		"missing command": {nil, 2, "Usage:"},
		"unknown command": {[]string{"unknown"}, 2, "unknown command: unknown"},
		"serve error":     {[]string{"serve"}, 1, "at least one --root is required"},
		"grant error":     {[]string{"grant"}, 1, "grant requires exactly one REQUEST_ID"},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(testCase.args, &stdout, &stderr)
			if code != testCase.wantCode {
				t.Fatalf("exit code = %d, want %d", code, testCase.wantCode)
			}
			if !strings.Contains(stderr.String(), testCase.wantText) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), testCase.wantText)
			}
		})
	}
}
