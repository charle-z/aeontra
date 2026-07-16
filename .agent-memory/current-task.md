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


## P11.2 validation checkpoint — 2026-07-15

- Validation branch functional commit: 1312344143e33caf4ba31772e31fde39d51343bd.
- Published checkpoint branch HEAD before the response-consume fix: a9bd665b03f26aa5894377c9c540eeee4cbdafd6.
- Last validated safe-diagnostic tree: bda3399624ee3851576a43c9e41898dfede358d6.
- Docker E2E run 29461855085 isolated the shared failure code response_consume.
- Current fix under validation makes the response-consumption CAS the first transaction statement, preventing SQLITE_BUSY_SNAPSHOT during concurrent cleanup.
- Target branch remains untouched at 201d4c7052f034fef8483d8d2af6aff76d6fe207.


## P11.2 driver-restart checkpoint — 2026-07-15

- Branch: `p11-2-step7-validation`.
- Functional HEAD: `6f315ef938ffdb3f79b9e05fc56746cb7bab9c85`.
- Validated tree: `093330a19dbfa00ee57450476c2167b0f8b5de13`.
- Target branch remains untouched at `201d4c7052f034fef8483d8d2af6aff76d6fe207`.
- Fixed driver restart during `WaitResponse`: cancellation now uses `http.ErrAbortHandler`, preventing a synthetic HTTP 200 with an empty body.
- Real Node provider + Unix driver + SQLite integration proves one create, bounded retries of only the same response wait, stable runtime/turn/sequence/digest/request_ref, one authoritative turn and one consumption.
- Green: driver cancellation 20x; provider restart 20x; provider suite; focused modelturn/edgeclient/mcpserver; `go test -p 1 ./... -count=1`; `go vet ./...`; `go build ./...`; `git diff --check`.
- CI: this HEAD has not yet been published or run through Docker E2E.
- Current blocker: none locally; next evidence is the Docker direct and distributed slices on the exact published SHA.
- Next exact commands: commit this checkpoint, push `p11-2-step7-validation`, then wait for `E2E distributed OpenCode` on the resulting SHA.


## P11.2 Docker E2E checkpoint — 2026-07-15

- Branch: `p11-2-step7-validation`.
- Published HEAD: `7897befc92369f0cc65ee2ec787588b3b1e3f487`.
- Exact tree: `687094f358efabc72af344b46f32c024bf87ad06`.
- Working tree before checkpoint: clean.
- Docker E2E run `29466504331`, check `87520668556`, failed on this exact SHA.
- The previous `response_identity` restart failure is fixed locally and in CI; the new closed diagnostic is `unknown_error` inside `TestOpenCodeExternalModelVerticalSlice`.
- The workflow preserved its bounds and cleanup; final exit was `124`, with no raw OpenCode output persisted.
- Last green local gate: `go test -p 1 ./... -count=1`, followed by `go vet ./...`, `go build ./...`, and `git diff --check`.
- Current blocker: classify the structured OpenCode `UnknownError` without exposing messages, then correct the underlying direct-slice cause.
- Next exact command: `git show 0887a88 --stat`, then compare the passing P11.1 provider/E2E implementation with the current tree.

## P11.2 deterministic remote provider checkpoint — 2026-07-16

- Active branch remains `p11-2-step7-validation`; target branch remains untouched at `201d4c7052f034fef8483d8d2af6aff76d6fe207`.
- Added `TestNodeProviderAgainstRemoteEdgeTransport` and `TestNodeProviderRetriesRemoteResponseAfterDriverRestart` with the real Node provider, real `model-turn-driver --remote`, real Unix socket, paired Ed25519 Edge identity, signed TLS `httptest`, separate authoritative store, and separate local journal.
- Reproduced the Docker `opencode_unknown_error` locally as a closed pre-create staging failure: remote staging returned private `lr_` references while the provider accepted only authoritative `mb_` references.
- Provider now accepts exactly the closed `mb_` or `lr_` reference prefixes; malformed prefixes still fail before create.
- Restart synchronization waits for the signed remote long-poll to be active, stops the driver, waits for release, recreates the socket, and proves only the same response GET is retried.
- Final remote integration command passed 20/20: `go test ./integrations/opencode/provider -run 'TestNodeProvider.*Remote' -count=20 -v`.
- Focused Edge/modelturn/MCP tests passed after the final synchronization.
- Final gates passed after this append: focused Edge/modelturn/MCP tests, `go test -p 1 ./... -count=1`, `go vet ./...`, `go build ./...`, tagged E2E compile-only, and `git diff --check`.
- Do not run Docker until the exact committed SHA is published. No merge, deployment, real pairing, Parrot installation, tag, or Coolify changes.
