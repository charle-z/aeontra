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

func runnerACLScript(t *testing.T, marker string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct{ Run string } `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, step := range workflow.Jobs["execution"].Steps {
		_, script, found := strings.Cut(step.Run, "<<'"+marker+"'\n")
		if !found {
			continue
		}
		script, _, found = strings.Cut(script, "\n"+marker)
		if !found {
			t.Fatalf("missing %s terminator", marker)
		}
		return script
	}
	t.Fatalf("missing %s script", marker)
	return ""
}

func runRunnerACLFixture(t *testing.T, script, fixture string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Linux rootless ACL fixtures require WSL Python")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	program := "scope = {'__name__': 'acl_fixture'}\nexec(" + fmt.Sprintf("%q", script) + ", scope)\n" + fixture
	command := exec.CommandContext(ctx, python, "-c", program)
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	output, err := command.CombinedOutput()
	if ctx.Err() != nil || err != nil || strings.TrimSpace(string(output)) != "fixture passed" {
		t.Fatalf("ACL fixture failed: deadline=%v err=%v output=%s", ctx.Err(), err, output)
	}
}

func TestDevelopmentRunnerACLParserPreservesNumericNamedIDs(t *testing.T) {
	script := runnerACLScript(t, "ACL_DIAGNOSTICS")
	runRunnerACLFixture(t, script, `
import struct
def acl(identifier):
    entries = [(1, 7, 4294967295), (2, 7, identifier), (4, 5, 4294967295), (16, 7, 4294967295), (32, 0, 4294967295)]
    return struct.pack('<I', 2) + b''.join(struct.pack('<HHI', *entry) for entry in entries)
for identifier in (0, 1000, 65534, 4294967295):
    raw = acl(identifier)
    result = scope['decode_acl'](raw)
    assert result['raw_hex'] == raw.hex()
    assert result['entries'][1] == ['user', identifier, 7]
    assert result['unmapped_named_id'] == (identifier == 4294967295)
group = struct.pack('<I', 2) + struct.pack('<HHI', 8, 5, 4294967295)
assert scope['decode_acl'](group)['unmapped_named_id']
for raw in (b'', bytes([2, 0, 0]), struct.pack('<I', 3), acl(1000)[:-1], b'x' * 257,
            struct.pack('<IHHI', 2, 64, 7, 0), struct.pack('<IHHI', 2, 2, 8, 0)):
    try: scope['decode_acl'](raw)
    except ValueError: pass
    else: raise AssertionError('invalid ACL accepted')
print('fixture passed')
`)
}

func TestDevelopmentRunnerACLNamespaceMapsAreNumericAndBounded(t *testing.T) {
	directory := t.TempDir()
	fixtures := map[string]string{
		"valid":      "0 1002 1\n1 100000 65536\n",
		"empty":      "",
		"short":      "0 1002\n",
		"negative":   "0 -1 1\n",
		"zero":       "0 1002 0\n",
		"nonnumeric": "0 workload 1\n",
		"oversized":  strings.Repeat("0 1002 1\n", 1000),
	}
	for name, body := range fixtures {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fixture := fmt.Sprintf("directory = %q\n", filepath.ToSlash(directory)) + `
import os
assert scope['read_map'](os.path.join(directory, 'valid')) == [[0, 1002, 1], [1, 100000, 65536]]
for name in ('empty', 'short', 'negative', 'zero', 'nonnumeric', 'oversized'):
    try: scope['read_map'](os.path.join(directory, name))
    except ValueError: pass
    else: raise AssertionError('invalid namespace map accepted')
print('fixture passed')
`
	runRunnerACLFixture(t, runnerACLScript(t, "ACL_DIAGNOSTICS"), fixture)
}

func TestDevelopmentRunnerACLImageDirectoryMayHaveNonzeroOwner(t *testing.T) {
	runRunnerACLFixture(t, runnerACLScript(t, "ACL_DIAGNOSTICS"), `
import struct, types
records = []
scope['emit'] = records.append
scope['os'].getuid = lambda: 0
scope['os'].fstat = lambda fd: types.SimpleNamespace(st_mode=scope['stat'].S_IFDIR | 0o1777, st_uid=1000, st_gid=1000)
raw = struct.pack('<IHHI', 2, 2, 7, 1000)
scope['os'].getxattr = lambda fd, name: raw
scope['inspect_fd'](42, 'buildkit-tests-tmp', require_owner=False)
assert records[0]['uid'] == 1000
assert records[1]['entries'] == [['user', 1000, 7]]
try: scope['inspect_fd'](42, 'docker-data-root')
except ValueError: pass
else: raise AssertionError('non-workload storage owner accepted')
scope['os'].getxattr = lambda fd, name: b'invalid'
try: scope['inspect_fd'](42, 'buildkit-tests-tmp', require_owner=False)
except ValueError: pass
else: raise AssertionError('malformed ACL did not fail diagnostic')
print('fixture passed')
`)
}

func TestDevelopmentRunnerACLHolderCleanupAfterReaderFailure(t *testing.T) {
	runRunnerACLFixture(t, runnerACLScript(t, "ACL_DIAGNOSTICS"), `
import builtins, io, types
image, cid = 'sha256:' + 'a' * 64, 'b' * 64
calls = []
scope['emit'] = lambda value: None
scope['os'].getuid = lambda: 1002
scope['pwd'].getpwnam = lambda name: types.SimpleNamespace(pw_uid=1002)
scope['read_map'] = lambda path: [[0, 0, 4294967295]]
scope['inspect_storage'] = lambda: None
scope['sys'].argv = ['runner-acl-diagnostics.py']
scope['os'].stat = lambda path: types.SimpleNamespace(st_ino=1)
def fake_directory_open(path, flags):
    assert path == '/proc/17/root/image-tmp'
    assert flags & scope['os'].O_NOFOLLOW and flags & scope['os'].O_DIRECTORY
    return 42
scope['os'].open = fake_directory_open
scope['os'].close = lambda fd: None
scope['inspect_fd'] = lambda fd, label, require_owner: calls.append(('read-directory', label, require_owner))
def fake_docker(*argv):
    calls.append(argv)
    if argv[0] == 'info': return '{}'
    if argv[0] == 'image': return image
    if argv[0] == 'run':
        assert '--pull=never' in argv and '--network=none' in argv and '--read-only' in argv
        assert 'type=image,src=' + image + ',dst=/image-tmp,image-subpath=tmp' in argv
        assert argv[-2:] == ('/bin/sleep', '45')
        return cid
    if argv[0] == 'inspect': return '17'
    assert argv == ('rm', '--force', cid)
    return cid
scope['docker'] = fake_docker
def fake_open(path):
    assert path == '/proc/17/status'
    return io.StringIO('Uid:\t1002\t1002\t1002\t1002\n')
builtins.open = fake_open
def fake_nsenter(argv, **kwargs):
    assert argv == ['/usr/bin/nsenter', '--target', '17', '--user', '--preserve-credentials', '--',
                    '/usr/bin/python3', '/opt/aeontra-bin/runner-acl-diagnostics.py', '--reader', '17', '1']
    assert kwargs['timeout'] == 10
    return types.SimpleNamespace(returncode=1, stdout=b'', stderr=b'')
scope['subprocess'].run = fake_nsenter
try: scope['main']()
except RuntimeError: pass
else: raise AssertionError('namespace read failure was hidden')
assert calls[-1] == ('rm', '--force', cid)
assert ('read-directory', 'buildkit-tests-tmp-initial', False) in calls
print('fixture passed')
`)
}

func TestDevelopmentRunnerACLReaderUsesActualNamespaceIdentity(t *testing.T) {
	runRunnerACLFixture(t, runnerACLScript(t, "ACL_DIAGNOSTICS"), `
import struct, types
records = []
scope['emit'] = records.append
scope['read_map'] = lambda path: [[0, 0, 1], [1, 100000, 65536]]
scope['os'].stat = lambda path: types.SimpleNamespace(st_ino=2)
scope['os'].open = lambda path, flags: 42
scope['os'].close = lambda fd: None
scope['os'].getuid = lambda: 0
scope['os'].fstat = lambda fd: types.SimpleNamespace(st_mode=scope['stat'].S_IFDIR | 0o1777, st_uid=1000, st_gid=1000)
scope['os'].getxattr = lambda fd, name: struct.pack('<IHHI', 2, 2, 7, 4294967295)
scope['reader'](17, 1)
assert records[0]['uid_map'][0] == [0, 0, 1], 'legitimate nested mapping rejected'
assert records[1]['directory'] == 'buildkit-tests-tmp-rootless'
assert records[2]['unmapped_named_id']
try: scope['reader'](17, 2)
except ValueError: pass
else: raise AssertionError('initial namespace accepted as rootless reader')
print('fixture passed')
`)
}

func TestDevelopmentRunnerACLRecordsHaveFiniteOutputBudget(t *testing.T) {
	runRunnerACLFixture(t, runnerACLScript(t, "ACL_DIAGNOSTICS"), `
import contextlib, io, json
capture = io.StringIO()
with contextlib.redirect_stdout(capture):
    scope['emit']({'oversized': 'x' * 4096})
    for i in range(1000): scope['emit']({'directory': 'workload-home', 'entry': i})
output = capture.getvalue()
assert 0 < len(output.encode()) <= 3800
assert 'oversized diagnostic record' in output
for line in output.splitlines():
    assert len(line) <= 1000
    assert isinstance(json.loads(line), dict)
print('fixture passed')
`)
}

func TestDevelopmentRunnerACLStorageDoesNotFollowDirectoryLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux O_DIRECTORY/O_NOFOLLOW storage fixtures require WSL")
	}
	for _, linked := range []string{"aeontra-workload", "docker"} {
		t.Run(linked, func(t *testing.T) {
			directory, outside := t.TempDir(), t.TempDir()
			if linked == "docker" {
				if err := os.Mkdir(filepath.Join(directory, "aeontra-workload"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(directory, "aeontra-workload")
			if linked == "docker" {
				path = filepath.Join(path, "docker")
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
			script := strings.ReplaceAll(runnerACLScript(t, "ACL_DIAGNOSTICS"), "'/home'", fmt.Sprintf("%q", directory))
			fixture := fmt.Sprintf("outside = %q\n", outside) + `
import os
outside_inode = os.stat(outside).st_ino
reads = []
def read_xattr(fd, key):
    assert os.fstat(fd).st_ino != outside_inode
    reads.append(key)
    raise OSError(61, 'fixture ACL absent')
scope['os'].getxattr = read_xattr
scope['emit'] = lambda value: None
try: scope['inspect_storage']()
except OSError: pass
else: raise AssertionError('linked directory accepted')
print('fixture passed')
`
			runRunnerACLFixture(t, script, fixture)
		})
	}
}

func TestDevelopmentRunnerACLWorkflowIsDiagnosticOnly(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name, ID, If, Run string
				Continue          any `yaml:"continue-on-error"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	commandIndex, diagnosticIndex, receiptIndex := -1, -1, -1
	for index, step := range workflow.Jobs["execution"].Steps {
		switch step.Name {
		case "Execute exact profile":
			commandIndex = index
			if step.ID != "exact-command" || step.Continue != nil {
				t.Fatal("exact command gate changed")
			}
		case "Inspect rootless storage ACL after exact make failure":
			diagnosticIndex = index
			if step.If != "${{ !cancelled() && steps.exact-command.outcome == 'failure' && inputs.command_profile == 'make-validate-all' }}" || step.Continue != nil || !strings.Contains(step.Run, "execute.py acl-diagnostics acl-diagnostics") {
				t.Fatal("diagnostic escaped the exact failed make profile")
			}
		case "Bind trusted receipt":
			receiptIndex = index
		}
	}
	if commandIndex < 0 || diagnosticIndex <= commandIndex || receiptIndex <= diagnosticIndex {
		t.Fatal("diagnostic must run after the command and before daemon teardown")
	}
	script := runnerACLScript(t, "ACL_DIAGNOSTICS")
	for _, forbidden := range []string{"setxattr", "removexattr", "--privileged", "--security-opt", "--storage-driver", "nocopy", "ci.json", "runtime.env", "ACTIONS_", "sudo", "docker.sock:/", "image', 'pull"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("ACL diagnostic added mutation or controller authority %q", forbidden)
		}
	}
	for _, required := range []string{"os.O_NOFOLLOW", "observed.st_uid != os.getuid()", "owner != os.getuid()", "--preserve-credentials", "--pull=never", "--network=none", "--read-only", "--memory=64m", "--pids-limit=16", "image-subpath=tmp", "'--reader'", "timeout=10", "budget = 3800", "'system.posix_acl_default'"} {
		if !strings.Contains(script, required) {
			t.Fatalf("missing bounded workload diagnostic invariant %q", required)
		}
	}
}

func TestDevelopmentRunnerACLExecutorDoesNotReadOrPassCITokens(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, script, found := strings.Cut(string(body), "sudo tee /opt/aeontra-control/execute.py >/dev/null <<'PYTHON'\n")
	if !found {
		t.Fatal("trusted executor missing")
	}
	script, _, found = strings.Cut(script, "\n          PYTHON")
	if !found {
		t.Fatal("trusted executor terminator missing")
	}
	script = strings.TrimPrefix(strings.ReplaceAll(script, "\n          ", "\n"), "          ")
	// Execute the extracted launcher against synthetic process/file fixtures;
	// no systemd, Docker, namespace or controller files are used by this test.
	program := `
import builtins, io, os, pwd, subprocess, sys, types
captured = {}
class FixtureFile(io.StringIO):
    def close(self): captured['environment'] = self.getvalue(); super().close()
def fixture_open(path, mode):
    assert path != '/opt/aeontra-control/ci.json', 'diagnostic read CI authority'
    if path.endswith('/runtime.env'): return FixtureFile()
    if path.endswith('/acl-diagnostics.log'): return io.BytesIO()
    raise AssertionError('unexpected file access')
builtins.open = fixture_open
os.chmod = lambda *args: None
pwd.getpwnam = lambda name: types.SimpleNamespace(pw_uid=1002)
os.environ['ACTIONS_RUNTIME_TOKEN'] = 'NEVER-INHERIT-CONTROLLER-TOKEN'
def fixture_popen(argv, **kwargs):
    captured['argv'] = argv
    assert kwargs['env'] == {'PATH': '/usr/bin:/bin'}
    return types.SimpleNamespace(stdout=io.BytesIO(b'fixture output'), wait=lambda **kwargs: 0)
subprocess.Popen = fixture_popen
sys.argv = ['execute.py', 'acl-diagnostics', 'acl-diagnostics']
`
	program += "\ntry: exec(" + fmt.Sprintf("%q", script) + ", {'__name__': 'executor_fixture'})\nexcept SystemExit as result: assert result.code == 0\n"
	program += `
assert 'User=aeontra-workload' in captured['argv']
assert 'RuntimeMaxSec=60' in captured['argv']
assert captured['argv'][-1] == '/opt/aeontra-bin/runner-acl-diagnostics.py'
assert 'ACTIONS_' not in captured['environment']
assert 'GITHUB_ACTIONS' not in captured['environment']
assert 'NEVER-INHERIT' not in captured['environment']
print('fixture passed')
`
	runRunnerACLFixture(t, "", program)
}

func TestDevelopmentRunnerACLLogUsesExistingBoundedRedaction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("existing Linux diagnostic renderer fixtures require WSL")
	}
	directory := t.TempDir()
	for name, body := range map[string]string{
		"ci.json":             `{"ACTIONS_RUNTIME_TOKEN":"private-token"}`,
		"acl-diagnostics.log": "private-token\n::error::injected\n" + strings.Repeat("ACL diagnostic\n", 4096),
		"runtime.env":         "NEVER-READ-ENVIRONMENT",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := runRunnerDiagnostics(t, directory)
	if err != nil || !strings.Contains(output, "aeontra-untrusted-log acl-diagnostics ") || len(output) > 16<<10 || strings.Contains(output, "private-token") || strings.Contains(output, "NEVER-READ") {
		t.Fatalf("unsafe ACL diagnostic rendering: err=%v output=%s", err, output)
	}
}

func TestDevelopmentRunnerACLRedactionKeepsPublicMetadataAndNumericEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("existing Linux diagnostic renderer fixtures require WSL")
	}
	directory := t.TempDir()
	values, err := json.Marshal(map[string]string{
		"GITHUB_REF":            "refs/heads/main",
		"GITHUB_REPOSITORY":     "charle-z/buildkit",
		"GITHUB_RUN_ID":         "37162950406",
		"GITHUB_RUN_ATTEMPT":    "1",
		"ACTIONS_RUNTIME_TOKEN": "private-token",
		"ACTIONS_RESULTS_URL":   "https://private-ci.example/cache",
		"UNKNOWN_PRIVATE_FIELD": "unknown-private-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	acl := `{"raw_hex":"0200000001000700ffffffff02000700e8030000","entries":[["user",1000,7]],"uid_map":[[0,1002,1],[1,100000,65536]]}`
	for name, body := range map[string][]byte{
		"ci.json":             values,
		"acl-diagnostics.log": []byte(acl + "\nprivate-token https://private-ci.example/cache unknown-private-value\n"),
	} {
		if err := os.WriteFile(filepath.Join(directory, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := runRunnerDiagnostics(t, directory)
	if err != nil {
		t.Fatalf("ACL renderer failed: %v: %s", err, output)
	}
	for _, private := range []string{"private-token", "https://private-ci.example/cache", "unknown-private-value"} {
		if strings.Contains(output, private) {
			t.Fatalf("private field %q escaped redaction", private)
		}
	}
	first := strings.Split(strings.TrimSpace(output), "\n")[0]
	encoded, found := strings.CutPrefix(first, "aeontra-untrusted-log acl-diagnostics ")
	var preserved string
	if !found || json.Unmarshal([]byte(encoded), &preserved) != nil || preserved != acl {
		t.Fatalf("public attempt metadata mutilated numeric ACL evidence: %s", output)
	}
}
