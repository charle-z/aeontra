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
