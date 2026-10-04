package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentRunnerNestedSubordinateMaps(t *testing.T) {
	runRunnerACLFixture(t, runnerACLScript(t, "NESTED_IDMAP"), `
def captured(body):
    return 'aeontra-capture bytes_seen=%d bytes_dropped=0 bytes_retained=%d\n%s' % (len(body), len(body), body)
valid = 'uid\n0 1002 1\n1 231072 262144\ngid\n0 1002 1\n1 231072 262144\n'
scope['verify_maps'](captured(valid), 1002, 1002)
# Reproduce the actual generation-8 map: inner IDs 100000..165535
# requested by BuildKit are outside the outer namespace.
invalid = [valid.replace('262144', '65536'), valid.replace('0 1002 1', '0 0 1'),
           valid.replace('1 231072', '1 0'), valid.replace('gid\n', 'uid\n'),
           valid.replace('0 1002 1', '0 1003 1'), valid.replace('1 231072 262144', '2 231072 262144'),
           valid + '1 900000 1\n', valid.replace('231072', '-1'),
           valid.replace('262144', '4294967295'), 'x' * 4097]
for value in invalid:
    try: scope['verify_maps'](captured(value), 1002, 1002)
    except ValueError: pass
    else: raise AssertionError('insufficient or identity-changing namespace accepted')
for value in (valid, captured(valid).replace('bytes_dropped=0', 'bytes_dropped=1'),
              captured(valid).replace('bytes_seen=', 'bytes_seen=1'), captured(valid)[:-1]):
    try: scope['verify_maps'](value, 1002, 1002)
    except ValueError: pass
    else: raise AssertionError('unframed or truncated probe capture accepted')
print('fixture passed')
`)
}

func TestDevelopmentRunnerNestedMapPrivateCapture(t *testing.T) {
	runRunnerACLFixture(t, runnerACLScript(t, "NESTED_IDMAP"), `
import os, tempfile
from pathlib import Path
with tempfile.TemporaryDirectory() as directory:
    path = Path(directory) / 'probe.log'
    path.write_text('fixed probe data')
    os.chmod(path, 0o644)
    try: scope['read_probe'](path, os.getuid())
    except ValueError: pass
    else: raise AssertionError('ordinary producer mode accepted without explicit chmod')
    os.chmod(path, 0o600)
    assert scope['read_probe'](path, os.getuid()) == 'fixed probe data'
    try: scope['read_probe'](path, os.getuid() + 1)
    except ValueError: pass
    else: raise AssertionError('wrong owner accepted')
    link = Path(directory) / 'link'
    link.symlink_to(path)
    try: scope['read_probe'](link, os.getuid())
    except OSError: pass
    else: raise AssertionError('symlink accepted')
print('fixture passed')
`)
}

func TestDevelopmentRunnerFreshAccountAndMeasuredNestedMap(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "development-runner.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"--key SUB_UID_COUNT=262144 --key SUB_GID_COUNT=262144",
		"nested-idmap docker run --rm --pull=never --network=none --read-only --memory 256m --pids-limit 64",
		"cat /proc/self/uid_map", "cat /proc/self/gid_map", "NESTED_IDMAP",
		"nested-idmap.log",
		"sudo chmod 0600 /opt/aeontra-control/nested-idmap.log",
	} {
		if !strings.Contains(string(body), required) {
			t.Errorf("missing fresh-account or actual namespace-map gate: %s", required)
		}
	}
}
