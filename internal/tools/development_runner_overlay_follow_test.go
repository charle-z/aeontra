package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentRunnerOverlayFollowSetupPrecedesRootlessDaemon(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	setup := strings.Index(text, "<<'OVERLAY_REDIRECT_POLICY'")
	daemon := strings.Index(text, "systemctl --user start aeontra-rootless.service")
	if setup < 0 || daemon < setup {
		t.Fatal("fixed overlay policy must be verified before starting rootless Docker")
	}
	script := runnerACLScript(t, "OVERLAY_REDIRECT_POLICY")
	for _, required := range []string{"/sys/module/overlay/parameters/redirect_always_follow", "os.O_NOFOLLOW", "os.geteuid()", "stat.S_ISREG", "st_uid", "os.read", "os.write"} {
		if !strings.Contains(script, required) {
			t.Fatalf("missing fixed VM-only overlay invariant %q", required)
		}
	}
	for _, forbidden := range []string{"subprocess", "modprobe", "environ", "redirect_dir", "sysctl", "ci.json"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("overlay setup added unrelated authority %q", forbidden)
		}
	}
}

func TestDevelopmentRunnerOverlayFollowSetupChangesOnlyFixedPolicy(t *testing.T) {
	runRunnerACLFixture(t, runnerACLScript(t, "OVERLAY_REDIRECT_POLICY"), `
import os, tempfile, types
scope['os'] = types.SimpleNamespace(**vars(os))
original_stat = scope['os'].fstat
scope['os'].geteuid = lambda: 0
scope['os'].fstat = lambda fd: types.SimpleNamespace(st_mode=original_stat(fd).st_mode, st_uid=0)
with tempfile.TemporaryDirectory() as directory:
    path = os.path.join(directory, 'parameter')
    for before in (b'Y\n', b'N\n'):
        with open(path, 'wb') as file: file.write(before)
        scope['configure_overlay_redirect_policy'](path)
        with open(path, 'rb') as file: assert file.read() == b'N\n'
print('fixture passed')
`)
}

func TestDevelopmentRunnerOverlayFollowSetupRejectsUnsafeOrUnknownParameter(t *testing.T) {
	runRunnerACLFixture(t, runnerACLScript(t, "OVERLAY_REDIRECT_POLICY"), `
import os, tempfile, types
scope['os'] = types.SimpleNamespace(**vars(os))
original_stat = scope['os'].fstat
scope['os'].geteuid = lambda: 0
scope['os'].fstat = lambda fd: types.SimpleNamespace(st_mode=original_stat(fd).st_mode, st_uid=0)
def rejected(path):
    try: scope['configure_overlay_redirect_policy'](path)
    except (OSError, RuntimeError): return
    raise AssertionError('unsafe parameter accepted')
with tempfile.TemporaryDirectory() as directory:
    path = os.path.join(directory, 'parameter')
    rejected(path)
    rejected(directory)
    with open(path, 'wb') as file: file.write(b'unknown\n')
    rejected(path)
    with open(path, 'rb') as file: assert file.read() == b'unknown\n'
    link = os.path.join(directory, 'link')
    os.symlink(path, link)
    rejected(link)
    with open(path, 'wb') as file: file.write(b'Y\n')
    scope['os'].fstat = lambda fd: types.SimpleNamespace(st_mode=original_stat(fd).st_mode, st_uid=1002)
    rejected(path)
    scope['os'].fstat = lambda fd: types.SimpleNamespace(st_mode=original_stat(fd).st_mode, st_uid=0)
    scope['os'].geteuid = lambda: 1002
    rejected(path)
    with open(path, 'rb') as file: assert file.read() == b'Y\n'
print('fixture passed')
`)
}

func TestDevelopmentRunnerOverlayFollowSetupDoesNotHideWriteFailure(t *testing.T) {
	runRunnerACLFixture(t, runnerACLScript(t, "OVERLAY_REDIRECT_POLICY"), `
import os, tempfile, types
scope['os'] = types.SimpleNamespace(**vars(os))
original_stat = scope['os'].fstat
scope['os'].geteuid = lambda: 0
scope['os'].fstat = lambda fd: types.SimpleNamespace(st_mode=original_stat(fd).st_mode, st_uid=0)
with tempfile.TemporaryDirectory() as directory:
    path = os.path.join(directory, 'parameter')
    with open(path, 'wb') as file: file.write(b'Y\n')
    for result in ('denied', 'short', 'not-applied'):
        def write(fd, value):
            assert value == b'N\n'
            if result == 'denied': raise PermissionError('read-only parameter')
            return 1 if result == 'short' else 2
        scope['os'].write = write
        try: scope['configure_overlay_redirect_policy'](path)
        except (OSError, RuntimeError): pass
        else: raise AssertionError('failed policy write accepted')
        with open(path, 'rb') as file: assert file.read() == b'Y\n'
print('fixture passed')
`)
}
