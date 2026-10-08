#!/bin/sh
set -eu

image="${1:-mcp-sandbox-workcell:ci}"
case "$image" in
  ''|*[!A-Za-z0-9._:/@-]*)
    echo "invalid sandbox workcell image" >&2
    exit 2
    ;;
esac

fixture="$(mktemp -d)"
cleanup() {
  rm -rf -- "$fixture"
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$fixture/go" "$fixture/rust/src" "$fixture/node" "$fixture/python" "$fixture/git"

cat >"$fixture/zlib-smoke.c" <<'EOF'
#include <string.h>
#include <zlib.h>

int main(void) {
    return strcmp(zlibVersion(), "1.3.2.1-motley") == 0 ? 0 : 1;
}
EOF

cat >"$fixture/go/go.mod" <<'EOF'
module example.test/sandbox-smoke

go 1.26
EOF
cat >"$fixture/go/smoke_test.go" <<'EOF'
package smoke

import "testing"

func TestRuntime(t *testing.T) {}
EOF

cat >"$fixture/rust/Cargo.toml" <<'EOF'
[package]
name = "sandbox-smoke"
version = "0.0.0"
edition = "2024"
EOF
cat >"$fixture/rust/src/lib.rs" <<'EOF'
#[cfg(test)]
mod tests {
    #[test]
    fn runtime() {
        assert_eq!(2 + 2, 4);
    }
}
EOF

cat >"$fixture/node/smoke.test.js" <<'EOF'
const test = require('node:test');
const assert = require('node:assert/strict');

test('runtime', () => assert.equal(2 + 2, 4));
EOF

cat >"$fixture/python/test_smoke.py" <<'EOF'
import unittest


class RuntimeTest(unittest.TestCase):
    def test_runtime(self):
        self.assertEqual(2 + 2, 4)
EOF

cat >"$fixture/run.sh" <<'EOF'
#!/bin/sh
set -eu
export HOME=/tmp/home
export GOCACHE=/tmp/go-cache
export GOMODCACHE=/tmp/go-mod-cache
export CARGO_HOME=/tmp/cargo-home
mkdir -p "$HOME" "$GOCACHE" "$GOMODCACHE" "$CARGO_HOME"
test "$(pwd)" = /workspace
printf 'sandbox-basic-exec\n'
true
if false; then
  echo "false unexpectedly succeeded" >&2
  exit 1
fi
git --version
node - <<'JAVASCRIPT'
const assert = require('node:assert/strict');
const root = '/usr/lib/node_modules/npm/node_modules/';
assert.equal(require(root + 'brace-expansion/package.json').version, '5.0.11');
assert.equal(require(root + 'undici/package.json').version, '6.28.1');
assert.deepEqual(require(root + 'brace-expansion').expand('{cat,dog}'), ['cat', 'dog']);
const undici = require(root + 'undici');
for (const name of ['Agent', 'EnvHttpProxyAgent', 'RetryAgent', 'fetch']) {
  assert.equal(typeof undici[name], 'function');
}
console.log('npm_runtime_modules=ready');
JAVASCRIPT
test "$(cat /usr/share/aeontra/security/zlib-gzwrite-fix)" = 4d03c63b8648ab83053a6f00d304a5d6f9aa1ed7
cc /workspace/zlib-smoke.c -lz -o /tmp/zlib-smoke
/tmp/zlib-smoke
(cd /workspace/git &&
  git init --quiet &&
  git config user.name "Aeontra CI" &&
  git config user.email "aeontra-ci@localhost" &&
  printf 'fixture\n' > fixture.txt &&
  git add fixture.txt &&
  git commit --quiet -m "test: initialize fixture" &&
  git switch --quiet -c test/devbox-environment &&
  git status --short &&
  git diff --check &&
  git switch --quiet - &&
  git branch -D test/devbox-environment)
(cd /workspace/go && go test ./...)
(cd /workspace/rust && cargo test --offline)
(cd /workspace/node && node --test smoke.test.js)
(cd /workspace/python && python3 -m unittest -v)
pip --version
python3 -m pip --version
python3 - <<'PYTHON'
import zipfile
import ssl
from pip._vendor import urllib3

assert ssl.OPENSSL_VERSION_INFO[:3] >= (3, 6, 5), ssl.OPENSSL_VERSION
ssl.create_default_context()
print('python_tls=ready')
assert urllib3.__version__ == '2.8.0', urllib3.__version__
files = {
    'aeontra_pip_smoke.py': 'VALUE = 42\n',
    'aeontra_pip_smoke-0.0.0.dist-info/METADATA':
        'Metadata-Version: 2.1\nName: aeontra-pip-smoke\nVersion: 0.0.0\n',
    'aeontra_pip_smoke-0.0.0.dist-info/WHEEL':
        'Wheel-Version: 1.0\nRoot-Is-Purelib: true\nTag: py3-none-any\n',
}
record = 'aeontra_pip_smoke-0.0.0.dist-info/RECORD'
files[record] = ''.join(f'{name},,\n' for name in [*files, record])
with zipfile.ZipFile('/tmp/aeontra_pip_smoke-0.0.0-py3-none-any.whl', 'w') as wheel:
    for name, content in files.items():
        wheel.writestr(name, content)
PYTHON
python3 -m pip install --no-index --no-deps --no-compile \
  --target /tmp/pip-smoke-target /tmp/aeontra_pip_smoke-0.0.0-py3-none-any.whl
PYTHONPATH=/tmp/pip-smoke-target python3 - <<'PYTHON'
import aeontra_pip_smoke

assert aeontra_pip_smoke.VALUE == 42
print('pip_runtime=ready')
PYTHON
printf 'sandbox_workcell_toolchains=ready\n'
EOF
chmod 0755 "$fixture/run.sh"

docker run --rm \
  --network none \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --pids-limit 64 \
  --memory 512m \
  --cpus 1 \
  --user "$(id -u):$(id -g)" \
  --tmpfs /tmp:rw,exec,nosuid,nodev,size=256m \
  --volume "$fixture:/workspace:rw" \
  --workdir /workspace \
  "$image" \
  sh /workspace/run.sh
