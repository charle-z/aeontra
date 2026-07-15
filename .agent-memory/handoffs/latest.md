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

P11 Step 2 is implemented locally as the candidate-only `workspace_checkpoint`
read operation. It advances the local catalog to 68 tools with hash
`sha256:86ab04ccb609b191aa2c471688100ed5c10a5641a81effba9a8c617fd3ba9c33`
without changing deployed P8.1 evidence. The measured fixture reduced two MCP calls
to one, response bytes from 2052 to 406, and repeated bytes from 260 to zero.

## Next safe step

Step 2 is committed. Step 3 adds persistent bounded telemetry/logs under the existing
state volume. Close its gates and commit, then add the bounded result store before
Edge device identity, one-use pairing
and leased-task replay tests. Do not expose a remote shell, pair a real device, install
WSL automatically, or begin Parrot/HTB authority.
