package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubConfigureReadsTokenOnlyFromStdinAndStatusIsSafe(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	token := "github_pat_abcdefghijklmnopqrstuvwxyz0123456789"
	var output bytes.Buffer
	if code := run([]string{"github", "configure", "--state", state, "--owner", "charle-z"}, strings.NewReader(token+"\n"), &output, &output); code != 0 {
		t.Fatalf("configure code=%d output=%s", code, output.String())
	}
	if strings.Contains(output.String(), token) || !strings.Contains(output.String(), `"configured":true`) || !strings.Contains(output.String(), `"owner":"charle-z"`) {
		t.Fatalf("unsafe configure output=%s", output.String())
	}
	output.Reset()
	if code := run([]string{"github", "status", "--state", state}, strings.NewReader(""), &output, &output); code != 0 {
		t.Fatalf("status code=%d output=%s", code, output.String())
	}
	if strings.Contains(output.String(), token) || !strings.Contains(output.String(), `"configured":true`) {
		t.Fatalf("unsafe status output=%s", output.String())
	}
}
