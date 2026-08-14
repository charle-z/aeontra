# Current task - signed Codex harness, then managed multiagent execution

Updated: 2026-08-14

## Verified starting point

- The dated operational baseline records production at 166 tools, the real
  `parrot-trusted-linux` Edge on signed release `p15.0.34`, and Front Door commit
  `489a64f40cbbde014986ff130662a485f9513d6c`.
- PR #180 merged the strict loopback Responses adapter used by stock Codex CLI.
- Branch `codex/codex-signed-harness` is based on the current `origin/main` and carries
  the signed-product candidate. No source merge, release, Edge update, or live Codex
  acceptance has been claimed yet.
- The candidate public identity is protocol `2024-11-05`, 167 tools and catalog
  `sha256:8ce8ca2897c7550546ba1277bbe590670c0d4d6648959b7362bc0bd9114cb523`.

## Active scope

The current change packages official Codex CLI `0.147.0` with an exact source, archive,
and binary digest. The signed Edge launcher starts a private loopback Responses adapter,
uses an isolated `CODEX_HOME`, clears credential-shaped environment variables, and runs
Codex inside the existing Bubblewrap workcell boundary. Codex internal agents remain
disabled until managed worktrees and one-writer fencing exist.

The installed updater currently understands manifest v3. The safe rollout therefore
requires two signed releases after exact-head CI and merge:

1. a bridge-v3 release that updates the Edge/updater while retaining OpenCode;
2. a codex-v4 release that adds the pinned Codex binary and pin manifest and activates
   Codex;
3. real-device doctor, process, model-turn, and rollback acceptance before removing the
   OpenCode compatibility path.

After that acceptance, continue in separate focused changes:

1. managed worktree creation, ownership, one-writer fencing, status, and cleanup;
2. association of P16 tasks with worktrees and runtimes;
3. bounded read-only multiagent fan-out, then isolated writers and deterministic merge;
4. failure, cancellation, restart, fairness, and real Edge acceptance.

## Boundaries

- Do not reset, clean, force-push, or discard unknown checkout state.
- Do not infer an installed release from source or CI.
- Do not expose ChatGPT, Codex, OpenAI, GitHub, or browser credentials to Codex turns.
- Keep the public MCP container free of host sockets and arbitrary host authority.
- Do not enable Codex built-in multiagent before managed worktrees and fencing pass.
- Retain OpenCode as rollback until one signed Codex release passes real-device
  acceptance.
