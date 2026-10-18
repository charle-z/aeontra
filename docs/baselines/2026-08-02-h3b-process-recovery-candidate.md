# Hito 3B process recovery candidate — 2026-08-02

## Identity

- Base: `84bac0a13bf71078e94e407f49f52e5758f3b872`.
- Branch: `codex/h3b-process-recovery`.
- Candidate catalog: 121 tools,
  `sha256:feca2e4d163cfcff7e08410d5d5b34a52396430d49515c0f601486ffde0b31e2`.

## Candidate behavior

The candidate adds `project_process_signal`, `project_process_list`, and
`project_process_cleanup`. Signal uses a closed enum. List is bounded and metadata-only.
Cleanup is explicit, idempotent and terminal-only. Manager reopen reconciles durable
rows against live Linux identity without polling while idle. A minimal worker from the
same signed Edge binary owns each background Bubblewrap pipe, redacts before
persistence, records terminal receipt, and survives Edge main-process restart;
foreground and OpenCode behavior remain unchanged.

## Evidence boundary

Focused Linux tests cover live recovery without duplicate start, offline exit, PID
reuse, foreign ownership, incomplete/unsafe logs, bounded list, closed signals and
live-preserving cleanup. This is candidate evidence only. Exact-head CI, merge,
deployment/catalog transition, signed release, installed Edge identity and real restart
acceptance must be appended in a later dated production baseline; they are not claimed
here.
