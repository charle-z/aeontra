package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentRunnerFreshHomeClearsOnlyInheritedACLs(t *testing.T) {
	runRunnerACLFixture(t, runnerACLScript(t, "FRESH_WORKLOAD_HOME"), `
import errno, os, struct, tempfile
from pathlib import Path
def acl(uid):
    entries = [(1, 7, 4294967295), (2, 7, uid), (4, 5, 4294967295), (16, 7, 4294967295), (32, 5, 4294967295)]
    return struct.pack('<I', 2) + b''.join(struct.pack('<HHI', *entry) for entry in entries)
def absent(path, name):
    try: os.getxattr(path, name)
    except OSError as error:
        assert error.errno == errno.ENODATA
        return
    raise AssertionError('inherited ACL still present')
with tempfile.TemporaryDirectory() as parent:
    raw = acl(os.getuid() + 1)
    os.setxattr(parent, 'system.posix_acl_default', raw)
    sibling = Path(parent) / 'application'
    sibling.mkdir()
    sibling_access = os.getxattr(sibling, 'system.posix_acl_access')
    # This is the prior runner preparation, including its ineffective chmod.
    inherited = Path(parent) / 'ordinary-home'
    inherited.mkdir(mode=0o700)
    os.chmod(inherited, 0o700)
    assert os.getxattr(inherited, 'system.posix_acl_default') == raw
    assert os.getxattr(inherited, 'system.posix_acl_access')
    uid, gid = (os.getuid(), os.getgid()) if os.getuid() else (1, 1)
    scope['prepare_home'](parent, 'workload', uid, gid)
    home = Path(parent) / 'workload'
    assert home.stat().st_uid == uid and home.stat().st_gid == gid
    assert home.stat().st_mode & 0o777 == 0o700
    for name in ('system.posix_acl_access', 'system.posix_acl_default'):
        absent(home, name)
    # Tests normally run as the workload UID; root can test a transferred home.
    child = home / 'runtime'
    child.mkdir()
    absent(child, 'system.posix_acl_default')
    absent(child, 'system.posix_acl_access')
    assert os.getxattr(parent, 'system.posix_acl_default') == raw
    assert os.getxattr(sibling, 'system.posix_acl_default') == raw
    assert os.getxattr(sibling, 'system.posix_acl_access') == sibling_access
print('fixture passed')
`)
}

func TestDevelopmentRunnerFreshHomeRejectsExistingEntries(t *testing.T) {
	runRunnerACLFixture(t, runnerACLScript(t, "FRESH_WORKLOAD_HOME"), `
import os, tempfile
from pathlib import Path
with tempfile.TemporaryDirectory() as parent:
    uid, gid = (os.getuid(), os.getgid()) if os.getuid() else (1, 1)
    directory = Path(parent) / 'directory'
    directory.mkdir()
    (directory / 'user-file').write_text('keep')
    file = Path(parent) / 'file'
    file.write_text('keep')
    (Path(parent) / 'symlink').symlink_to(directory, target_is_directory=True)
    for name in ('directory', 'file', 'symlink'):
        try: scope['prepare_home'](parent, name, uid, gid)
        except FileExistsError: pass
        else: raise AssertionError('reused existing entry')
    assert file.read_text() == 'keep'
    assert (directory / 'user-file').read_text() == 'keep'
    for invalid in ('../escape', '/absolute', '.', 'nested/path'):
        try: scope['prepare_home'](parent, invalid, uid, gid)
        except ValueError: pass
        else: raise AssertionError('invalid leaf accepted')
    for uid, gid in ((0, 1), (1, 0), (-1, 1)):
        try: scope['prepare_home'](parent, 'invalid-owner', uid, gid)
        except ValueError: pass
        else: raise AssertionError('invalid owner accepted')
print('fixture passed')
`)
}

func TestDevelopmentRunnerFreshHomeFailsClosedOnACLFailure(t *testing.T) {
	for _, operation := range []string{"removexattr", "getxattr"} {
		t.Run(operation, func(t *testing.T) {
			runRunnerACLFixture(t, runnerACLScript(t, "FRESH_WORKLOAD_HOME"), `
import errno, os, tempfile
operation = `+`"`+operation+`"`+`
original = getattr(os, operation)
def failed(*args, **kwargs): raise OSError(errno.EPERM, 'ACL access denied')
with tempfile.TemporaryDirectory() as parent:
    uid, gid = (os.getuid(), os.getgid()) if os.getuid() else (1, 1)
    setattr(os, operation, failed)
    try:
        try: scope['prepare_home'](parent, 'workload', uid, gid)
        except OSError as error: assert error.errno == errno.EPERM
        else: raise AssertionError('ACL error suppressed')
    finally: setattr(os, operation, original)
    assert os.stat(parent + '/workload').st_uid == os.getuid()
print('fixture passed')
`)
		})
	}
}

func TestDevelopmentRunnerFreshHomePreparationAndVolumeProbe(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	prepare := string(body)
	if !strings.Contains(prepare, "useradd --no-create-home --home-dir /home/aeontra-workload") ||
		strings.Contains(prepare, "useradd --create-home") ||
		strings.Index(prepare, "FRESH_WORKLOAD_HOME") > strings.Index(prepare, "/home/aeontra-workload/runtime") {
		t.Fatal("fresh home must be prepared before any workload descendants")
	}
	probe := string(body)
	if !strings.Contains(probe, "volume-copy docker run --rm --pull=never --network=none --read-only --memory 256m --pids-limit 64 --mount type=volume,dst=/tmp busybox:1.37.0@sha256:") ||
		!strings.Contains(probe, "volume-copy.log") {
		t.Fatal("calibration must test actual rootless anonymous volume population")
	}
}
