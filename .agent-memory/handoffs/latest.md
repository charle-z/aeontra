# Latest handoff — MCP Devbox

Date: 2026-07-14
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
and local pairing/revocation commands. It does not yet authorize or execute work.

## Next safe step

Close Step 5 verification and commit it. Then implement the leased, idempotent Edge
task protocol with heartbeat, cancellation, expiry and reconnect tests before the
single outbound-only `development` WSL workcell. Do not expose a remote shell, pair
a real device, install WSL automatically, or begin Parrot/HTB authority.
