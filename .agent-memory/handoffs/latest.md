# Handoff — retryable remote runtime lease delivery

Branch: `codex/runtime-lease-observability`, based on `origin/main`
`d6f449f93d226d8e16238aabb784ab21f4b1a103`.

Observed production evidence: runtime `mr_9dbeb91135a6c9c9445e8dc214d34748` was
created in `awaiting_edge` at `2026-07-27T01:30:59.862Z`, had `last_sequence: 0`, and
was terminally failed at `2026-07-27T01:31:02.182Z`. The paired Edge id was correct.
On Parrot, `mcp-edge doctor` and the exact owner systemd service were active and the
correlated journal window was empty. This is not evidence that OpenCode itself ran or
failed.

The likely server path is `internal/edge/model_relay_http.go`: `writeLease` called
`FailRuntime` whenever `RuntimeGoal` could not retrieve the private goal body, then
returned HTTP 409. That made the Edge stop rather than retry before any model turn.

Candidate change: return HTTP 503, move `starting` back to `awaiting_edge`, and allow
the existing receipt to reserve it again. The regression test advances the private
goal clock beyond its TTL and asserts two lease attempts both receive 503 while the
runtime remains `ready/awaiting_edge`.

Parrot WSL2 passed the focused regression test, `go test ./internal/edge -count=1`,
`go test ./... -count=1`, `go vet ./...`, and `go build ./...`; `git diff --check` is
clean. Before merge: inspect the exact-head CI results, then deploy normally and
request one fresh dev continuation. Expected recovery signal:
the runtime advances beyond sequence zero or returns a distinct terminal runtime result;
do not reuse an old terminal runtime.
