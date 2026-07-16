# Current task

Date: 2026-07-16
Branch: `p11-2-remote-opencode-relay`
HEAD before Step 8 commit: `ef2cb7eee4ecb67e5526fc1d055a482edd92877e`
Step 7 tree: `f13fbcdb28ead77a4a17530a452f88249f9d6135`
Base `origin/main`: `01fde5067752ab1c43424d2d54f9afd914617ba5`

Historical deployed baseline retained for release synchronization: P8.1 Console 2.0 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`. P9 Brain is deployed. P11.2 does not modify those releases.

Immediate objective: publish the single Step 8 candidate, obtain authoritative GitHub Actions Bubblewrap/Docker evidence, fix by amend/force-push if necessary, then complete Step 9 documentation/baseline/benchmark and open the final green PR. No merge or deployment.

Step 8 candidate implementation:
- OpenCode runs only through mandatory, fail-closed Bubblewrap; no direct fallback exists.
- Bubblewrap must be an absolute regular executable, not a symlink and not writable by group/others.
- The effective Bubblewrap argv is parsed into a complete sandbox spec and validated before execution.
- Exact command validation binds the process to `/mcp-opencode run --auto --model bridge/external-model --format json --dir /workspace <lease goal>`.
- Exact provider JSON validation binds `runtimeID`, timeout, `/runtime/d.sock`, `file:///mcp-provider`, the external model, autoupdate false and exact permission denies for `external_directory`, `webfetch`, `websearch`.
- Mandatory flags: `--die-with-parent`, `--new-session`, `--unshare-all`, `--clearenv`.
- Effective mounts: workspace RW, runtime RW, provider RO, native OpenCode executable RO, required system/tool paths RO, `/proc`, `/dev`, private `/tmp`.
- Edge state/journal, host home, `/root`, `/mnt/c`, `/mnt/d`, Docker sockets and arbitrary writable mounts are absent.
- ToolPath is locally closed: fixed system directories or verified managed directories beneath `/srv/mcp-devbox-tools`; no remote arbitrary path.
- OpenCode version verification itself runs through Bubblewrap.
- The Docker E2E uses the pinned native `opencode-linux-x64` 1.18.1 binary rather than the npm shim, installs Bubblewrap from Debian, and remains non-root, read-only, cap-drop ALL, network none, without privileged mode, Docker socket or host PID/IPC.
- Real tagged smoke executes the test binary inside Bubblewrap and proves workspace/runtime writes, provider read-only, state/home/Docker/WSL paths hidden, TCP/DNS blocked, Unix socket reachable and user namespace active. It writes a redacted structured isolation report through `/runtime` and measures five Bubblewrap startup samples.
- Distributed E2E retains direct normal/restart and remote normal/driver-restart, and records four unique role PIDs while explicitly excluding the `bwrap` parent from OpenCode detection.

Local candidate gates green:
- `go fmt ./...`
- `go test -p 1 ./... -count=1`
- `go vet ./...`
- `go build ./...`
- coverage generation and `cmd/coverage-gate`
- Staticcheck v0.7.0
- Govulncheck v1.6.0: no vulnerabilities found
- Actionlint v1.7.12
- tagged edgeclient and mcpserver E2E binaries compile
- `git diff --check`

Environment-limited gates:
- real Bubblewrap smoke fails immediately on this host because `bwrap` is not installed; the Docker E2E image installs it from Debian and is the authoritative environment.
- local race cannot compile because this host lacks GCC; the official Race detector job installs GCC and runs with CGO.
- Docker is not exposed through the public MCP interface and privileged task profiles are disabled.

Next action: remove `.agent-scratch/`, stage the exact candidate, record the tree, commit `Step 8: isolate OpenCode runtime with Bubblewrap`, push the branch and inspect every CI result. If the real E2E fails, amend the same commit and force-push so no diagnostic history remains.
