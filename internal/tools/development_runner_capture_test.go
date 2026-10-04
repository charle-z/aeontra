package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runnerOutputCaptureScript(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, script, found := strings.Cut(string(body), "def capture_output(stream, log):\n")
	if !found {
		t.Fatal("bounded final-output capture missing")
	}
	script, _, found = strings.Cut(script, "          workload_uid =")
	if !found {
		t.Fatal("capture function boundary missing")
	}
	return "def capture_output(stream, log):\n" + strings.TrimPrefix(strings.ReplaceAll(script, "\n          ", "\n"), "          ")
}

func TestDevelopmentRunnerCaptureRetainsActualFinalFailure(t *testing.T) {
	program := "import io\n" + runnerOutputCaptureScript(t) + `
output = io.BytesIO()
capture_output(io.BytesIO(b'old output\n' * (2 << 20) + b'error: final fork failure\nmake: exit 2\n'), output)
data = output.getvalue()
assert len(data) <= (16 << 20) + 256
assert data.endswith(b'error: final fork failure\nmake: exit 2\n')
assert b'bytes_dropped=0 ' not in data.split(b'\n', 1)[0]
class BrokenStream:
    def __init__(self): self.calls = 0
    def read(self, limit):
        self.calls += 1
        if self.calls == 1: return b'error: before read failure\n'
        raise OSError('fixture failure')
partial = io.BytesIO()
try: capture_output(BrokenStream(), partial)
except OSError: pass
else: raise AssertionError('read failure was suppressed')
assert partial.getvalue().endswith(b'error: before read failure\n')
print('fixture passed')
`
	runRunnerACLFixture(t, "", program)
}

func TestDevelopmentRunnerDiagnosticsShowsTailCaptureMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hosted Linux diagnostic fixtures are verified in WSL")
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "ci.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	body := "aeontra-capture bytes_seen=17000000 bytes_dropped=300000 bytes_retained=16700000\n" +
		strings.Repeat("old context\n", 200000) + "panic: final failure\n"
	if err := os.WriteFile(filepath.Join(directory, "command.log"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := runRunnerDiagnostics(t, directory)
	if err != nil || !strings.Contains(output, "bytes_dropped=300000") || !strings.Contains(output, "panic: final failure") || len(output) > 16<<10 {
		t.Fatalf("capture metadata or final failure lost: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(directory, "ci.json"), []byte(`{"UNKNOWN_PRIVATE_FIELD":"17000000"}`), 0600); err != nil {
		t.Fatal(err)
	}
	output, err = runRunnerDiagnostics(t, directory)
	if err != nil || strings.Contains(output, "17000000") || !strings.Contains(output, "bytes_seen=[REDACTED]") {
		t.Fatalf("numeric capture credential not redacted: %v: %s", err, output)
	}
}

func TestDevelopmentRunnerCapturePreservesCompleteLineBoundaries(t *testing.T) {
	program := "import io\n" + runnerOutputCaptureScript(t) + `
for body, expected in (
    (b'first complete line\n' + b'z' * ((16 << 20) - 20), b'first complete line\n'),
    (b'private-token' + b'p' * (16 << 20) + b'\nsafe failure\n', b'safe failure\n'),
    (b'p' * ((16 << 20) + 1), b''),
):
    output = io.BytesIO()
    capture_output(io.BytesIO(body), output)
    metadata, data = output.getvalue().split(b'\n', 1)
    assert data.startswith(expected), data[:100]
    if expected != b'first complete line\n': assert b'p' not in data
    assert b'bytes_seen=' in metadata and b'bytes_dropped=' in metadata
print('fixture passed')
`
	runRunnerACLFixture(t, "", program)
}

func TestDevelopmentRunnerResourceCountersBoundedAndScoped(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, script, found := strings.Cut(string(body), "<<'RESOURCE_COUNTERS'\n")
	if !found {
		t.Fatal("pre-teardown resource capture missing")
	}
	script, _, found = strings.Cut(script, "\n          RESOURCE_COUNTERS")
	if !found {
		t.Fatal("resource capture terminator missing")
	}
	script = strings.TrimPrefix(strings.ReplaceAll(script, "\n          ", "\n"), "          ")
	directory := t.TempDir()
	group := filepath.Join(directory, "rootless")
	if err := os.Mkdir(group, 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"pids.current": "12\n", "pids.max": "4096\n", "pids.events": "max 17\n",
		"memory.current": "4096\n", "memory.max": "10737418240\n", "memory.events": "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\noom_group_kill 0\n",
	} {
		if err := os.WriteFile(filepath.Join(group, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	script = strings.ReplaceAll(script, "'/sys/fs/cgroup'", fmt.Sprintf("%q", filepath.ToSlash(directory)))
	script = strings.ReplaceAll(script, "'/opt/aeontra-control/resources.log'", fmt.Sprintf("%q", filepath.ToSlash(filepath.Join(directory, "resources.log"))))
	fixture := `import pwd, subprocess, types
pwd.getpwnam = lambda name: types.SimpleNamespace(pw_uid=1002)
def show(argv, **kwargs):
    assert argv[:2] == ['/usr/bin/systemctl', 'show']
    assert kwargs['timeout'] == 3
    unit = argv[2]
    assert unit in ('user@1002.service', 'aeontra-command.service')
    return types.SimpleNamespace(returncode=0, stdout=b'/rootless\n' if unit == 'user@1002.service' else b'', stderr=b'')
subprocess.run = show
`
	runRunnerACLFixture(t, "", fixture+"\n"+script+"\nprint('fixture passed')\n")
	result, err := os.ReadFile(filepath.Join(directory, "resources.log"))
	if err != nil || !strings.Contains(string(result), `"max": 17`) || !strings.Contains(string(result), `"state": "unavailable"`) || len(result) > 4096 {
		t.Fatalf("resource counters missing or unbounded: %v: %s", err, result)
	}
	// Delegation must not let a workload redirect the controller's reader.
	if err := os.Remove(filepath.Join(group, "pids.current")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("NEVER-READ-TARGET"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(group, "pids.current")); err != nil {
		t.Fatal(err)
	}
	runRunnerACLFixture(t, "", fixture+"\n"+script+"\nprint('fixture passed')\n")
	result, err = os.ReadFile(filepath.Join(directory, "resources.log"))
	if err != nil || !strings.Contains(string(result), `"state": "unavailable"`) || strings.Contains(string(result), "NEVER-READ") {
		t.Fatalf("unsafe resource counter accepted: %v: %s", err, result)
	}
	if err := os.Remove(filepath.Join(group, "pids.current")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(group, "pids.current"), []byte(strings.Repeat("1", 1025)), 0600); err != nil {
		t.Fatal(err)
	}
	runRunnerACLFixture(t, "", fixture+"\n"+script+"\nprint('fixture passed')\n")
	result, err = os.ReadFile(filepath.Join(directory, "resources.log"))
	if err != nil || strings.Contains(string(result), `"state": "observed"`) {
		t.Fatalf("oversized resource counter accepted: %v: %s", err, result)
	}
	// A traversal or replaced cgroup directory must be unavailable as well.
	for _, path := range []string{"/../rootless", "/linked"} {
		if path == "/linked" {
			if err := os.Symlink(group, filepath.Join(directory, "linked")); err != nil {
				t.Fatal(err)
			}
		}
		changed := strings.ReplaceAll(fixture, "b'/rootless\\n'", fmt.Sprintf("%q", path+"\n"))
		changed = strings.ReplaceAll(changed, fmt.Sprintf("%q", path+"\n"), "b"+fmt.Sprintf("%q", path+"\n"))
		runRunnerACLFixture(t, "", changed+"\n"+script+"\nprint('fixture passed')\n")
		result, err = os.ReadFile(filepath.Join(directory, "resources.log"))
		if err != nil || strings.Contains(string(result), `"state": "observed"`) {
			t.Fatalf("unsafe cgroup path accepted: %v: %s", err, result)
		}
	}
}
