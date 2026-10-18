# Hito 3A background-process release candidate — 2026-08-02

Status: **validation pending**. This is candidate evidence, not a deployment or an
installed-Edge claim.

## Scope

The candidate extends direct GPT Web execution with `project_process_start`,
`project_process_status`, and `project_process_stop`. It uses the same trusted-workcell
Bubblewrap process specification as foreground `project_exec`; it does not start
OpenCode or another model runtime.

Candidate catalog identity: 118 tools,
`sha256:d8b6b35f7f3dc7ef13a16d98130c0d129a50cf64f2e6216cecc39e3e1b1829ab`.

The Edge owns a private SQLite process journal and separate redacted `0600` output
files. The public surface exposes an opaque process id, closed states, timestamps,
bounded incremental output and terminal status, never PID, process group, argv,
environment, local paths or secret material. Start is durable-idempotent. Stop verifies
PID start time and process group, then uses TERM and bounded KILL escalation.

## Local evidence

On the Windows checkout through Parrot WSL2 and Go 1.26.5, the focused Edge, client,
MCP server, command, catalog, integration and documentation suites passed. The matrix
includes split-write redaction, truncation, zero/non-zero exit, concurrency, cwd and
symlink rejection, cross-project denial, PID-reuse defense and repeated stop.
Private log reads also reject symlink substitution and revalidate the same opened inode.

The focused CGO race detector passed for `internal/edge`, `internal/edgeclient` and
`internal/mcpserver`. The complete suite passed every package except the existing
DrvFS executable-mode assertion in `packaging/builder`: the Windows mount presents the
checked-in script as `0777` instead of its Linux Git mode `0755`. Vet and build passed;
Linux exact-head CI remains the authority for that host-specific permission gate.

The full suite, vet, build, exact-head GitHub gates, merge commit, backend/Front Door
catalog transition, signed Edge release, real-device installation and live process
acceptance are intentionally still pending. No later evidence may rewrite this file to
claim those events happened on this candidate date.
