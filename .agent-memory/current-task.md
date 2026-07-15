# Current task

P11.2 branch `p11-2-remote-opencode-relay`, based exactly on `origin/main` `01fde5067752ab1c43424d2d54f9afd914617ba5`. Do not merge, deploy, tag, pair a real Edge, install on Parrot, or change Coolify.

Historical production baseline retained for synchronized release evidence: **P8.1 is deployed** at merge `d343264bffdc0ae1bc045a9d723e913be977090c`; P9 Brain and its deployed P8.1 successor remain documented independently. This does not claim that P11.2 is deployed.

Committed:

- Step 1 `97f9956ed40cfba1bb1f9e3e6c7c465daa96075b` — bind authoritative model runtimes to opaque Edge device/workspace identities.
- Step 2 `efdd2693c2a23a3b58dd8f716eebbc06ef6186ac` — private local Edge workspace registry with revalidation.
- Step 3 `bf4ed04a2e5a43b1daaa3db71ebcbd332b29d806` — signed device-bound runtime/turn relay endpoints.
- Step 4 `056dc3cc9848cafda9f0639f68babcfa46dae6fe` — generic model-turn driver transport plus restart-safe `RemoteEdgeTransport` and minimal local idempotency journal.

Step 5 is implemented and validated in the working tree, ready for commit `Step 5: launch pinned OpenCode runtimes on Edge`:

- exact `opencode-ai@1.18.1` version and pinned npm integrity are checked locally before every runtime;
- the only provider is the local `@mcp-devbox/opencode-external-driver`, with no model/provider fallback;
- `mcp-edge opencode` accepts only local administrator paths and bounded timing/output settings; the VPS lease supplies only opaque runtime/workspace identity, a bounded goal, timeout and fixed provider profile;
- workspace IDs resolve through the local registry and are revalidated immediately before launch;
- non-root execution is enforced; private state/socket roots are 0700 and the Unix socket is 0600; there is no TCP listener or public OpenCode server;
- argv and environment are constructed internally, inherited AI/provider credentials are absent, browser auth/model fetching/updates/downloads/sharing are disabled;
- a 0600 local SQLite journal enforces one active runtime per workspace, duplicate-goal rejection, terminal replay protection and restart-safe no-double-execution behavior without storing prompts, paths, output or secrets;
- timeout, remote cancellation, heartbeat, local STOP kill switch, process-group termination, driver startup failure and bounded transient output are handled safely;
- adversarial tests cover missing/deleted/symlinked/ownership-changed workspaces, concurrent/duplicate/completed runtimes, root refusal, version/integrity/provider drift, insecure socket, unexpected exit, cancellation, timeout, kill switch, restart and oversized output.

Step 5 gates passed on this exact tree:

- `go fmt ./...`;
- focused `internal/edgeclient`, `cmd/mcp-edge` and `internal/edge` tests;
- complete configured project test suite (`run_tests`, all Go packages and provider Node tests);
- `go vet ./...`;
- `go build ./...`;
- `git diff --check`.

No `tmp_*.go` helper remains. Next action: commit Step 5, record its exact SHA, then immediately implement Step 6 bounded MCP controls and deliberate catalog/hash update.
