package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func runnerDiagnosticScript(t *testing.T, directory string) (string, string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("run hosted Linux diagnostic fixtures in WSL")
		}
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct{ Name, Run string } `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, step := range workflow.Jobs["execution"].Steps {
		if step.Name != "Bounded untrusted command diagnostics" {
			continue
		}
		_, script, found := strings.Cut(step.Run, "<<'PYTHON'\n")
		if !found {
			t.Fatal("missing diagnostic Python body")
		}
		script, _, found = strings.Cut(script, "\nPYTHON")
		if !found {
			t.Fatal("missing diagnostic Python terminator")
		}
		return python, strings.ReplaceAll(script, "'/opt/aeontra-control/'", fmt.Sprintf("%q", filepath.ToSlash(directory)+"/"))
	}
	t.Fatal("missing trusted diagnostic step")
	return "", ""
}

func runRunnerDiagnostics(t *testing.T, directory string) (string, error) {
	t.Helper()
	python, script := runnerDiagnosticScript(t, directory)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, python, "-c", script)
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("diagnostics exceeded deadline: %v", ctx.Err())
	}
	return string(output), err
}

func TestDevelopmentRunnerDiagnosticsBeforeCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hosted Linux diagnostic fixtures require native Python; verified in WSL")
	}
	directory := t.TempDir()
	for name, body := range map[string]string{
		"ci.json":              `{"ACTIONS_RUNTIME_TOKEN":"private-token","ACTIONS_RESULTS_URL":"private-url"}`,
		"docker-info.log":      "Cannot connect to Docker: private-token private-url\n::error::injected\n\x1b[31mred\n",
		"rootless-service.log": "daemon startup failed\n",
		"runtime.env":          "NEVER-PRINT-PRIVATE-ENV",
		"unknown.log":          "NEVER-PRINT-UNREGISTERED-LOG",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := runRunnerDiagnostics(t, directory)
	if err != nil || !strings.Contains(output, "Cannot connect to Docker") || !strings.Contains(output, "daemon startup failed") {
		t.Fatalf("failed probe diagnostics lost before command: %v: %s", err, output)
	}
	for _, forbidden := range []string{"private-token", "private-url", "NEVER-PRINT", "\x1b"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("unsafe diagnostic output contains %q", forbidden)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if !strings.HasPrefix(line, "aeontra-untrusted-log ") {
			t.Fatalf("unescaped runner channel output: %q", line)
		}
		_, encoded, found := strings.Cut(strings.TrimPrefix(line, "aeontra-untrusted-log "), " ")
		var text string
		if !found || json.Unmarshal([]byte(encoded), &text) != nil {
			t.Fatalf("diagnostic line is not prefixed JSON: %q", line)
		}
	}
}

func TestDevelopmentRunnerDiagnosticsBoundedAndRejectsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hosted Linux no-follow diagnostic fixtures are verified in WSL")
	}
	t.Run("bounded", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "ci.json"), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"command", "rootless-service", "docker-info", "workspace-bind", "cache-export", "cache-import"} {
			if err := os.WriteFile(filepath.Join(directory, name+".log"), []byte(strings.Repeat("\x01 giant diagnostic\n", 4096)), 0600); err != nil {
				t.Fatal(err)
			}
		}
		output, err := runRunnerDiagnostics(t, directory)
		if err != nil || len(output) == 0 || len(output) > 16<<10 {
			t.Fatalf("encoded diagnostics are not bounded: bytes=%d err=%v", len(output), err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "ci.json"), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		secret := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(secret, []byte("NEVER-READ-SYMLINK-TARGET"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(secret, filepath.Join(directory, "docker-info.log")); err != nil {
			t.Fatal(err)
		}
		output, err := runRunnerDiagnostics(t, directory)
		if err == nil || strings.Contains(output, "NEVER-READ") {
			t.Fatalf("unsafe diagnostic link accepted: err=%v output=%s", err, output)
		}
	})
	t.Run("truncated credential", func(t *testing.T) {
		directory := t.TempDir()
		token := strings.Repeat("p", 256)
		credentials, _ := json.Marshal(map[string]string{"ACTIONS_RUNTIME_TOKEN": token})
		if err := os.WriteFile(filepath.Join(directory, "ci.json"), credentials, 0600); err != nil {
			t.Fatal(err)
		}
		body := token + strings.Repeat("y", 4000) + "\nsafe complete line\n"
		if err := os.WriteFile(filepath.Join(directory, "docker-info.log"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		output, err := runRunnerDiagnostics(t, directory)
		if err != nil || !strings.Contains(output, "safe complete line") || strings.Contains(output, "ppp") {
			t.Fatalf("truncated credential suffix exposed: err=%v output=%s", err, output)
		}
	})
	t.Run("nonregular", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "ci.json"), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(directory, "docker-info.log"), 0700); err != nil {
			t.Fatal(err)
		}
		if output, err := runRunnerDiagnostics(t, directory); err == nil {
			t.Fatalf("nonregular diagnostic log accepted: %s", output)
		}
	})
}
