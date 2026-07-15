# Latest handoff — MCP Devbox

Date: 2026-07-15
Branch: `codex/p11-edge-core`
Base: deployed and tagged P8.1 merge `d343264bffdc0ae1bc045a9d723e913be977090c`

## Current phase

P8.1 is closed. PR #10, all required remote gates, existing-application deployment,
catalog/Brain/console production smokes and annotated tag `p8.1` are complete. The
deployed public catalog remains 67 tools with hash
`sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed`.
The console reports Edge as `not_paired`; no Edge runtime is claimed.
Historical candidate evidence remains immutable; production closure is recorded in
`docs/baselines/2026-07-14-p8_1-production.md`.

P11 Steps 2-4 are committed locally. They add the compact workspace checkpoint,
bounded telemetry/operational logs, and a redacted bounded result store. The local
catalog is 71 tools with hash
`sha256:7dfa9bb83c935c7df875740102dafa5572852e5e8cb6c064c89c1e3acb5e30ac`.
No candidate-only change has been deployed.

Step 5 implements Edge device identity and one-time pairing under `/state/edge`:
Ed25519 per-device credentials, ten-minute one-use pairing codes, signed requests,
persistent nonce replay rejection, revocation, an isolated `/edge/v1/pair` route,
and local pairing/revocation commands. Step 6 adds the signed leased-task transport:
structured bounded objectives, per-device idempotency, reconnect-safe leases,
heartbeat, cancellation, terminal replay protection, and local admin commands. It
still executes no work. Step 7 adds the separately installed outbound `mcp-edge`
client, Bubblewrap-only development validation, persistent pre-execution journal,
heartbeat cancellation, local kill switch, WSL systemd unit and human setup guide.

## Next safe step

Publish only `codex/p11-edge-core`, open the P11 PR and require every remote gate on
the exact final SHA. Do not merge, deploy, tag, pair, install WSL, expose a remote
shell, or begin Parrot/HTB authority. After a human merge and exact deployment, the
next owner action is the dedicated WSL/Bubblewrap procedure in
`docs/install-edge-wsl.md`.
