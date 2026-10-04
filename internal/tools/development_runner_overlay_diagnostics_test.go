package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestDevelopmentRunnerOverlayDiagnosticsStayOptionalAndBounded(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name, If, Run string
				Continue      any `yaml:"continue-on-error"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, step := range workflow.Jobs["execution"].Steps {
		if step.Name != "Inspect rootless overlay compatibility" {
			continue
		}
		found = true
		if step.If != "${{ !cancelled() && (inputs.command_profile == 'probe-only' || (steps.exact-command.outcome == 'failure' && inputs.command_profile == 'make-validate-all')) }}" || step.Continue != nil {
			t.Fatal("overlay diagnostic changed admission or hid infrastructure failures")
		}
		if !strings.Contains(step.Run, "execute.py overlay-diagnostics overlay-diagnostics") {
			t.Fatal("missing fixed workload diagnostic invocation")
		}
	}
	if !found {
		t.Fatal("missing isolated overlay compatibility diagnostic")
	}
	script := runnerACLScript(t, "OVERLAY_DIAGNOSTICS")
	for _, required := range []string{"timeout=30", "timeout=5", "--volumes", "--network=none", "--read-only", "--memory=256m", "--pids-limit=64", "type=volume,dst=/tmp", "--pull=never", "pids.events", "memory.events", "os.O_NOFOLLOW"} {
		if !strings.Contains(script, required) {
			t.Fatalf("missing bounded overlay diagnostic invariant %q", required)
		}
	}
	for _, forbidden := range []string{"ci.json", "ACTIONS_", "--storage-driver", "sysctl", "removexattr", "docker.sock:/", "--net=host", "--pid=host"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("overlay diagnostic added authority or host mutation %q", forbidden)
		}
	}
	probe := runnerACLScript(t, "OVERLAY_PROBE")
	for _, required := range []string{"syscall.Mount", "redirect_dir=off", "userxattr", "syscall.Errno", "syscall.Unmount", "redirect_always_follow", "metacopy", "index", "os.ReadFile"} {
		if !strings.Contains(probe, required) {
			t.Fatalf("missing overlay A/B evidence %q", required)
		}
	}
}

func TestDevelopmentRunnerOverlayMountRejectionIsNotCommandAcceptance(t *testing.T) {
	script := runnerACLScript(t, "OVERLAY_DIAGNOSTICS")
	runRunnerACLFixture(t, script, `
import json, types
calls, records = [], []
scope['os'].getuid = lambda: 1002
scope['pwd'].getpwnam = lambda name: types.SimpleNamespace(pw_uid=1002)
scope['counters'] = lambda: {'pids.events': {'max': 0}, 'memory.events': {'oom': 0}}
scope['emit'] = records.append
def run(argv, **kwargs):
    calls.append(argv)
    assert kwargs['env'] == {'HOME': '/home/aeontra-workload', 'PATH': '/usr/bin:/bin', 'DOCKER_HOST': 'unix:///home/aeontra-workload/runtime/docker.sock'}
    if argv[1] == 'create':
        assert kwargs['timeout'] == 5
        return types.SimpleNamespace(returncode=0, stdout=b'c' * 64 + b'\n')
    if argv[1] == 'start':
        assert argv[2:] == ['--attach', 'c' * 64]
        assert kwargs['timeout'] == 30
        return types.SimpleNamespace(returncode=0, stdout=b'{"mode":"default","mounted":false,"errno":22}\n')
    assert argv[1:] == ['rm', '--force', '--volumes', 'c' * 64]
    assert kwargs['timeout'] == 5
    return types.SimpleNamespace(returncode=0, stdout=b'')
scope['subprocess'].run = run
scope['main']()
assert len(calls) == 3
assert records[1] == {'mode': 'default', 'mounted': False, 'errno': 22}
assert records[0]['phase'] == 'before' and records[-1]['phase'] == 'after'
assert all('acceptance' not in record and 'capability' not in record for record in records)
print('fixture passed')
`)
}

func TestDevelopmentRunnerOverlayTimeoutStillCleansCapturedContainer(t *testing.T) {
	script := runnerACLScript(t, "OVERLAY_DIAGNOSTICS")
	runRunnerACLFixture(t, script, `
import subprocess, types
calls = []
scope['os'].getuid = lambda: 1002
scope['pwd'].getpwnam = lambda name: types.SimpleNamespace(pw_uid=1002)
scope['counters'] = lambda: {}
scope['emit'] = lambda record: None
def run(argv, **kwargs):
    calls.append(argv)
    if argv[1] == 'create': return types.SimpleNamespace(returncode=0, stdout=b'c' * 64 + b'\n')
    if argv[1] == 'start': raise subprocess.TimeoutExpired(argv, 30)
    assert argv[1:] == ['rm', '--force', '--volumes', 'c' * 64]
    return types.SimpleNamespace(returncode=0, stdout=b'')
scope['subprocess'].run = run
try: scope['main']()
except subprocess.TimeoutExpired: pass
else: raise AssertionError('Docker timeout was hidden')
assert len(calls) == 3
print('fixture passed')
`)
}

func TestDevelopmentRunnerOverlayCreationFailureDoesNotDeleteForeignContainer(t *testing.T) {
	script := runnerACLScript(t, "OVERLAY_DIAGNOSTICS")
	runRunnerACLFixture(t, script, `
import subprocess, types
calls = []
scope['os'].getuid = lambda: 1002
scope['pwd'].getpwnam = lambda name: types.SimpleNamespace(pw_uid=1002)
scope['counters'] = lambda: {}
scope['emit'] = lambda record: None
def run(argv, **kwargs):
    calls.append(argv)
    assert argv[1] == 'create', 'failed creation triggered start or deletion'
    return types.SimpleNamespace(returncode=125, stdout=b'')
scope['subprocess'].run = run
try: scope['main']()
except RuntimeError: pass
else: raise AssertionError('creation failure was hidden')
assert len(calls) == 1
print('fixture passed')
`)
}

func TestDevelopmentRunnerOverlayProbeCompilesWithoutSourceDependencies(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux syscall fixture verified in WSL")
	}
	directory := t.TempDir()
	file := filepath.Join(directory, "probe.go")
	if err := os.WriteFile(file, []byte(runnerACLScript(t, "OVERLAY_PROBE")), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(directory, "probe"), file)
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GO111MODULE=off", "CGO_ENABLED=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixed overlay probe does not compile: %v: %s", err, output)
	}
}

func TestDevelopmentRunnerOverlayLogKeepsBoundedRedaction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux diagnostic renderer verified in WSL")
	}
	directory := t.TempDir()
	for name, body := range map[string]string{
		"ci.json":                 `{"ACTIONS_RUNTIME_TOKEN":"private-token"}`,
		"overlay-diagnostics.log": "private-token\n{\"mode\":\"default\",\"errno\":22}\n",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := runRunnerDiagnostics(t, directory)
	if err != nil || !strings.Contains(output, "overlay-diagnostics") || strings.Contains(output, "private-token") || len(output) > 16<<10 {
		t.Fatalf("overlay diagnostic lost or unsafe: %v: %s", err, output)
	}
}
