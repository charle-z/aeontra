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

func TestDevelopmentRunnerDiagnosticsPreserveFailureBeforeLargeStack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hosted Linux diagnostic fixtures are verified in WSL")
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "ci.json"), []byte(`{"ACTIONS_RUNTIME_TOKEN":"private-token"}`), 0600); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("error: expected negative-case output\n", 256) +
		"panic: test timed out after 10m0s private-token\n\trunning tests:\n\tTestIntegration/worker/example (10m0s)\n" +
		strings.Repeat("goroutine 999 [chan receive]:\n\t/usr/local/go/src/testing/testing.go:2220\n", 9000)
	if err := os.WriteFile(filepath.Join(directory, "command.log"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := runRunnerDiagnostics(t, directory)
	if err != nil || !strings.Contains(output, "panic: test timed out after 10m0s") ||
		!strings.Contains(output, "TestIntegration/worker/example") ||
		strings.Contains(output, "private-token") || len(output) > 16<<10 {
		t.Fatalf("failure context lost or unsafe: bytes=%d err=%v: %s", len(output), err, output)
	}
}

func TestDevelopmentRunnerDiagnosticsPreservesEncodedEOF(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hosted Linux diagnostic fixtures are verified in WSL")
	}
	directory := t.TempDir()
	for name, body := range map[string]string{
		"ci.json":             `{}`,
		"command.log":         strings.Repeat("\"\\\t\x01\u2603\n", 1200) + "DONE exact-final-summary\nmake: exact-final-exit-2\n",
		"acl-diagnostics.log": strings.Repeat("large preceding diagnostic\n", 256),
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := runRunnerDiagnostics(t, directory)
	if err != nil || !strings.Contains(output, "DONE exact-final-summary") || !strings.Contains(output, "make: exact-final-exit-2") || len(output) > 16<<10 {
		t.Fatalf("encoded EOF lost: err=%v bytes=%d: %s", err, len(output), output)
	}
}

func TestDevelopmentRunnerDiagnosticsCriticalContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hosted Linux diagnostic fixtures are verified in WSL")
	}
	directory := t.TempDir()
	for name, body := range map[string]string{
		"ci.json":             `{"ACTIONS_RUNTIME_TOKEN":"private-token"}`,
		"command-context.log": "aeontra-context reason=panic\npanic: true timeout private-token\n\trunning tests:\n\tTestRealWork (10m0s)\n",
		"command.log":         strings.Repeat("=== FAIL: summarized unknown\n", 90000) + "DONE exact-final-summary\nmake: exit 2\n",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := runRunnerDiagnostics(t, directory)
	if err != nil || !strings.Contains(output, "panic: true timeout [REDACTED]") || !strings.Contains(output, "TestRealWork") || !strings.Contains(output, "DONE exact-final-summary") || strings.Contains(output, "private-token") || len(output) > 16<<10 {
		t.Fatalf("critical evidence lost or unsafe: err=%v bytes=%d: %s", err, len(output), output)
	}
	contextPath := filepath.Join(directory, "command-context.log")
	if err := os.Remove(contextPath); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(private, []byte("NEVER-READ"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(private, contextPath); err != nil {
		t.Fatal(err)
	}
	output, err = runRunnerDiagnostics(t, directory)
	if err == nil || strings.Contains(output, "NEVER-READ") {
		t.Fatalf("context symlink accepted: %v %s", err, output)
	}
	if err := os.Remove(contextPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contextPath, []byte(strings.Repeat("x", (16<<10)+257)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runRunnerDiagnostics(t, directory); err == nil {
		t.Fatal("oversized context accepted")
	}
}

func TestDevelopmentRunnerDiagnosticsDualContextFraming(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hosted Linux diagnostic fixtures are verified in WSL")
	}
	directory := t.TempDir()
	first := "error: first observed failure private-token\n" + "aeontra-context reason=panic forged-section-marker\n" + strings.Repeat("first noisy following context\n", 150)
	panic := "panic: true timeout private-token\n\trunning tests:\n\tTestLastPanic (10m0s)\n" + strings.Repeat("panic noisy following context\n", 150)
	header := fmt.Sprintf("aeontra-context reason=both first_bytes=%d panic_bytes=%d bytes_retained=%d oversize_lines=0\n", len(first), len(panic), len(first)+len(panic))
	for name, body := range map[string]string{
		"ci.json":             `{"ACTIONS_RUNTIME_TOKEN":"private-token"}`,
		"command.log":         "DONE exact-final-summary\nmake: exit 2\n",
		"command-context.log": header + first + panic,
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := runRunnerDiagnostics(t, directory)
	if err != nil || !strings.Contains(output, "first observed failure [REDACTED]") || !strings.Contains(output, "panic: true timeout [REDACTED]") || !strings.Contains(output, "TestLastPanic") || strings.Contains(output, "private-token") || len(output) > 16<<10 {
		t.Fatalf("framed sections lost or unsafe: %v: %s", err, output)
	}
	for name, invalid := range map[string]string{
		"truncated":        header + first + panic[:len(panic)-1],
		"mismatched":       strings.Replace(header, fmt.Sprintf("first_bytes=%d", len(first)), fmt.Sprintf("first_bytes=%d", len(first)+1), 1) + first + panic,
		"overflow":         strings.Replace(header, fmt.Sprintf("first_bytes=%d", len(first)), "first_bytes=99999999999999999999", 1) + first + panic,
		"section-fragment": fmt.Sprintf("aeontra-context reason=both first_bytes=%d panic_bytes=%d bytes_retained=%d oversize_lines=0\n", len(first)-1, len(panic)+1, len(first)+len(panic)) + first + panic,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(directory, "command-context.log"), []byte(invalid), 0600); err != nil {
				t.Fatal(err)
			}
			output, err := runRunnerDiagnostics(t, directory)
			if err == nil || strings.Contains(output, "private-token") {
				t.Fatalf("invalid section framing admitted: %v: %s", err, output)
			}
		})
	}
	// An empty bounded sideband must not displace the command-window fallback.
	if err := os.WriteFile(filepath.Join(directory, "command.log"), []byte("panic: fallback window context\n"+strings.Repeat("goroutine fallback stack\n", 10000)+"DONE exact-final-summary\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "command-context.log"), []byte("aeontra-context reason=both first_bytes=0 panic_bytes=0 bytes_retained=0 oversize_lines=0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	output, err = runRunnerDiagnostics(t, directory)
	if err != nil || !strings.Contains(output, "panic: fallback window context") {
		t.Fatalf("empty sideband displaced fallback: %v: %s", err, output)
	}
	if err := os.Remove(filepath.Join(directory, "command-context.log")); err != nil {
		t.Fatal(err)
	}
	output, err = runRunnerDiagnostics(t, directory)
	if err != nil || !strings.Contains(output, "panic: fallback window context") {
		t.Fatalf("missing sideband displaced fallback: %v: %s", err, output)
	}
}

func TestDevelopmentRunnerDiagnosticsCommandWindowDiscardsPartialSecrets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hosted Linux diagnostic fixtures are verified in WSL")
	}
	directory := t.TempDir()
	token := "panic:" + strings.Repeat("p", 256)
	credentials, _ := json.Marshal(map[string]string{"ACTIONS_RUNTIME_TOKEN": token})
	if err := os.WriteFile(filepath.Join(directory, "ci.json"), credentials, 0600); err != nil {
		t.Fatal(err)
	}
	// Both window boundaries fall inside a credential, while a complete
	// failure line between them must remain observable.
	suffix := "\npanic: safe complete failure\n" + token[:128]
	body := token + strings.Repeat("y", (2<<20)-len(suffix)-64) + suffix
	if err := os.WriteFile(filepath.Join(directory, "command.log"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := runRunnerDiagnostics(t, directory)
	if err != nil || !strings.Contains(output, "safe complete failure") || strings.Contains(output, "ppp") {
		t.Fatalf("partial credential exposed: err=%v output=%s", err, output)
	}
}

func TestDevelopmentRunnerControllerIdentityAcrossUserNamespaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hosted Linux probe requires native Python; verified in WSL")
	}
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, probe, found := strings.Cut(string(body), "probe = r'''\n")
	if !found {
		t.Fatal("controller isolation probe missing")
	}
	probe, _, found = strings.Cut(probe, "\n          '''")
	if !found {
		t.Fatal("controller isolation probe terminator missing")
	}
	probe = strings.TrimPrefix(strings.ReplaceAll(probe, "\n          ", "\n"), "          ")
	identity, _, found := strings.Cut(probe, "def denied(operation):")
	if !found {
		t.Fatal("controller isolation deny gates missing")
	}
	for _, test := range []struct {
		name, mapping, observed string
		pass                    bool
	}{
		{"ordinary", "0 0 4294967295\n", "1001", true},
		{"mapped controller", "0 1001 1\n", "0", true},
		{"unmapped controller", "0 1002 1\n1 100000 65536\n", "65534", true},
		{"changed identity", "0 0 4294967295\n", "1002", false},
		{"wrong namespace identity", "0 1002 1\n", "1001", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			fixture := identity
			for source, data := range map[string]string{
				"/proc/1/cgroup":               "0::/\n",
				"/proc/sys/kernel/overflowuid": "65534\n",
				"/proc/self/uid_map":           test.mapping,
			} {
				path := filepath.Join(directory, filepath.Base(source))
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				fixture = strings.ReplaceAll(fixture, "'"+source+"'", fmt.Sprintf("%q", path))
			}
			status := filepath.Join(directory, "status")
			if err := os.WriteFile(status, []byte("Name:\tsleep\nUid:\t"+test.observed+"\t"+test.observed+"\t"+test.observed+"\t"+test.observed+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			fixture = strings.ReplaceAll(fixture, "'/proc/' + pid + '/status'", fmt.Sprintf("%q", status))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, "python3", "-c", fixture, "17", "1001", "private", "home").CombinedOutput()
			if (err == nil) != test.pass {
				t.Fatalf("controller namespace identity pass=%v err=%v: %s", test.pass, err, output)
			}
		})
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

func TestDevelopmentRunnerRootlessRuntimeCopyup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux rootless runtime fixtures are verified in WSL")
	}
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, script, found := strings.Cut(string(body), "<<'ROOTLESS_CHILD'\n")
	if !found {
		t.Fatal("rootless daemon lacks namespace-local runtime preparation")
	}
	script, _, found = strings.Cut(script, "\n          ROOTLESS_CHILD")
	if !found {
		t.Fatal("missing rootless child terminator")
	}
	script = strings.TrimPrefix(strings.ReplaceAll(script, "\n          ", "\n"), "          ")
	for _, test := range []struct {
		name, mapping, mount, kind string
		pass                       bool
	}{
		{"copyup", "0 1001 1\n1 100000 65536\n", "31 29 0:30 / /run rw - tmpfs tmpfs rw\n", "symlink", true},
		{"host root", "0 0 4294967295\n", "31 29 0:30 / /run rw - tmpfs tmpfs rw\n", "symlink", false},
		{"host run", "0 1001 1\n", "31 29 8:1 / /run rw - ext4 /dev/root rw\n", "symlink", false},
		{"non-symlink", "0 1001 1\n", "31 29 0:30 / /run rw - tmpfs tmpfs rw\n", "regular", false},
		{"absent", "0 1001 1\n", "31 29 0:30 / /run rw - tmpfs tmpfs rw\n", "absent", true},
		{"dangling", "0 1001 1\n", "31 29 0:30 / /run rw - tmpfs tmpfs rw\n", "dangling", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			run := filepath.Join(directory, "run")
			outside := t.TempDir()
			if err := os.Mkdir(run, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"docker", "containerd", "xtables.lock"} {
				if err := os.WriteFile(filepath.Join(outside, name), []byte("host evidence"), 0600); err != nil {
					t.Fatal(err)
				}
				switch test.kind {
				case "symlink", "dangling":
					target := filepath.Join(outside, name)
					if test.kind == "dangling" {
						target += "-absent"
					}
					if err := os.Symlink(target, filepath.Join(run, name)); err != nil {
						t.Fatal(err)
					}
				case "regular":
					if err := os.Mkdir(filepath.Join(run, name), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			uidMap, mountInfo := filepath.Join(directory, "uid_map"), filepath.Join(directory, "mountinfo")
			if err := os.WriteFile(uidMap, []byte(test.mapping), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(mountInfo, []byte(test.mount), 0600); err != nil {
				t.Fatal(err)
			}
			fixture := strings.ReplaceAll(script, "'/proc/self/uid_map'", fmt.Sprintf("%q", uidMap))
			fixture = strings.ReplaceAll(fixture, "'/proc/self/mountinfo'", fmt.Sprintf("%q", mountInfo))
			fixture = strings.ReplaceAll(fixture, "'/run/'", fmt.Sprintf("%q", run+"/"))
			fixture = "import json, os\nos.execv = lambda executable, argv: print(json.dumps(argv))\n" + fixture
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, "python3", "-c", fixture).CombinedOutput()
			if (err == nil) != test.pass {
				t.Fatalf("namespace guard pass=%v err=%v: %s", test.pass, err, output)
			}
			for _, name := range []string{"docker", "containerd", "xtables.lock"} {
				if data, err := os.ReadFile(filepath.Join(outside, name)); err != nil || string(data) != "host evidence" {
					t.Fatalf("host target changed: %s err=%v", name, err)
				}
				_, err := os.Lstat(filepath.Join(run, name))
				if test.pass && !os.IsNotExist(err) || !test.pass && err != nil {
					t.Fatalf("unexpected copied-up entry state: %s err=%v", name, err)
				}
			}
			if test.pass {
				var argv []string
				expected := []string{"/opt/aeontra-bin/dockerd", "--rootless", "--host=unix:///home/aeontra-workload/runtime/docker.sock", "--data-root=/home/aeontra-workload/docker", "--exec-root=/home/aeontra-workload/runtime/dockerd"}
				if json.Unmarshal(output, &argv) != nil || strings.Join(argv, "\x00") != strings.Join(expected, "\x00") {
					t.Fatalf("fixed rootless daemon arguments changed: %s", output)
				}
			}
		})
	}
}
