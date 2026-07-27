# Current task

Branch: `codex/runtime-lease-observability`, based on `origin/main` at
`d6f449f93d226d8e16238aabb784ab21f4b1a103`.

## Candidate

The remote Edge can be paired, bundle-valid and locally active while a newly created
continuation ends at sequence zero. The captured production trace shows that the
runtime changes from `awaiting_edge` to `failed` in roughly two seconds, before a
model turn is offered. The local systemd journal has no matching OpenCode failure.

The server relay previously treated an unavailable private runtime-goal body during
lease delivery as HTTP 409 and immediately marked the runtime `failed`. This candidate
returns a retryable HTTP 503, restores the runtime to `awaiting_edge`, and permits the
same signed lease receipt to reserve it again. It adds a focused regression test for
an expired/unavailable goal body and the receipt retry path. This changes no public
tool, authorization, secret, filesystem, or command boundary.

## Validation

Parrot WSL2 verified the focused regression test, `go test ./internal/edge -count=1`,
`go test ./... -count=1`, `go vet ./...`, and `go build ./...`. `git diff --check` is
clean. Exact-head CI and a fresh live continuation smoke remain required before
deployment.
