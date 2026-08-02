# Handoff — Hito 3A background-process candidate

Branch `codex/h3a-background-processes` is based on clean `origin/main` at
`b419d9dfd888a216813bca05aafa5d4de28f0196`.

The candidate adds three direct public tools and three signed Edge operation kinds for
background start/status/stop. The Edge implementation reuses the foreground workcell
process-spec builder, stores private durable metadata/logs, redacts before persistence,
returns bounded incremental output, enforces request idempotency and stops only a
PID-start-time-verified owned process group. It adds no model runtime and leaves the
OpenCode fallback intact.

Focused suites and the focused CGO race matrix are green in Parrot WSL2 Go 1.26.5,
including catalog contracts and split-chunk secret redaction. The complete suite passed
apart from the known DrvFS `0777`/Linux `0755` packaging-mode mismatch; vet, build and
native `git diff --check` are green. No commit, PR, deployment, release or Edge update exists
for this candidate yet. The next exact action is final diff review and commit. Linux CI
is authoritative for the known DrvFS executable-mode fixture.

The pre-existing real Edge service incident was reconciled before implementation: one
unmanaged old process held the instance lock while systemd retried. After terminating
only the verified stale PID, systemd acquired the lock and doctor reported one managed
active process on `p15.0.12`. Recheck live service identity before real acceptance; do
not repeat a repair if doctor remains ready.
