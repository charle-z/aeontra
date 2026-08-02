# Handoff — Hito 3B process recovery candidate

Remote `main` and production backend are at
`84bac0a13bf71078e94e407f49f52e5758f3b872`. The stable Front Door is healthy and the
public contract is the 118-tool Hito 3A catalog. The real Edge is still the older signed
`p15.0.12` bundle, so Hito 3A real-device acceptance remains pending.

Current branch `codex/h3b-process-recovery` is based exactly on that remote main. It
adds closed signal/list/cleanup process operations, bounded public summaries, explicit
terminal-only cleanup, restart reconciliation, owner/PID/start-ticks/group/log
validation, a signed per-process redaction/receipt worker, and background process
survival across Edge service restart. No model
runtime, OpenCode task, autopilot, subagent or additional authentication path was used.

Focused tests, vet, build and the CGO race matrix are green. The full suite has only
the known local DrvFS `0777`/Linux `0755` fixture mismatch; Linux CI is authoritative.
Candidate catalog identity is 121 tools,
`sha256:feca2e4d163cfcff7e08410d5d5b34a52396430d49515c0f601486ffde0b31e2`.
The remaining safe sequence is full gates, diff review, commit, publish, exact-head CI,
merge, Front Door two-catalog transition, backend deployment, old-catalog retirement,
signed release, one Edge update and real restart recovery acceptance.

The immutable release version is the only known external authorization constraint. An
attempt to infer `p15.0.13` was rejected before dispatch; no release was created. Obtain
explicit approval for the exact next version rather than bypassing the workflow gate.
