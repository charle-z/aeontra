# Current task

P11.2 branch `p11-2-remote-opencode-relay`, based exactly on `origin/main` `01fde5067752ab1c43424d2d54f9afd914617ba5`. Do not merge, deploy, tag, pair a real Edge, install on Parrot, or change Coolify.

Historical production baseline retained for synchronized release evidence: **P8.1 is deployed** at merge `d343264bffdc0ae1bc045a9d723e913be977090c`; P9 Brain and its deployed P8.1 successor remain documented independently. This does not claim that P11.2 is deployed.

Committed:

- Step 1 `97f9956ed40cfba1bb1f9e3e6c7c465daa96075b` — bind authoritative model runtimes to opaque Edge device/workspace identities.
- Step 2 `efdd2693c2a23a3b58dd8f716eebbc06ef6186ac` — private local Edge workspace registry with revalidation.
- Step 3 `bf4ed04a2e5a43b1daaa3db71ebcbd332b29d806` — signed device-bound runtime/turn relay endpoints.
- Step 4 `056dc3cc9848cafda9f0639f68babcfa46dae6fe` — restart-safe remote Edge model-turn transport.
- Step 5 `548bbb980930af3bac3d1a927825e54e1ea3f96c` — pinned OpenCode 1.18.1 launcher on Edge.

Step 6 is implemented and validated in the working tree, ready for commit `Step 6: expose bounded remote OpenCode controls`:

- added only `opencode_runtime_start`; reused and tightened `model_runtime_status` and `model_runtime_cancel` instead of duplicating OpenCode status/cancel tools;
- exact closed input is `device_id`, `workspace_id`, `goal`, `timeout_seconds`, `idempotency_key`; forbidden path/shell/argv/env/provider/key/model/mount/UID/VPN/sudo/network-policy fields are rejected before capability execution;
- runtime creation requires an active paired Edge device, binds an opaque local workspace, uses `remote_edge`, stages the bounded goal authoritatively, and is idempotent by caller key;
- public runtime responses contain only runtime/state/device/workspace/controller/last-sequence/update-time and optional result ref; goal, prompt, turn, command, path, IP and secret metadata are omitted;
- cancel is idempotent and returns the same bounded public view on replay;
- staged duplicate/conflicting goals are removed only when no authoritative runtime references them;
- fixed provider profile is centralized as `modelturn.OpenCodeProviderProfile` and remains absent from public input;
- catalog is deliberately 78 tools with hash `sha256:9a20218d912bd2f6f42a254145d97c976cfcdd581f89340d563c1642e03318ed`;
- historical catalog identities remain explicitly tested: 71 tools, 72 tools, and Step 4's 77 tools/hash `sha256:3f4e1812bd72a0508eba108d97dfd353ea9abc4c883cded262abd768f1f94518`.

Step 6 gates passed on this exact tree:

- `go fmt ./...`;
- focused MCP/modelturn/Edge/app/integration/catalog/Brain smoke suites;
- complete configured project suite including provider Node tests;
- `go vet ./...`;
- `go build ./...`;
- `git diff --check`.

No `tmp_*.go` helper remains. Next action: commit Step 6, record its exact SHA, then immediately implement Step 7 distributed OpenCode relay E2E and restart/resume validation without shared authoritative SQLite, filesystems, sockets, or memory.
