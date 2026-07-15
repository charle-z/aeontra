# Current task

P11.2 branch `p11-2-remote-opencode-relay`, based exactly on `origin/main` `01fde5067752ab1c43424d2d54f9afd914617ba5`. Do not merge, deploy, tag, pair a real Edge, install on Parrot, or change Coolify.

Committed:

- Step 1 `97f9956ed40cfba1bb1f9e3e6c7c465daa96075b` — bind authoritative model runtimes to opaque Edge device/workspace identities.
- Step 2 `efdd2693c2a23a3b58dd8f716eebbc06ef6186ac` — private local Edge workspace registry with revalidation.
- Step 3 `bf4ed04a2e5a43b1daaa3db71ebcbd332b29d806` — signed device-bound runtime/turn relay endpoints.

Step 4 is implemented and validated in the working tree, ready for the coherent commit `Step 4: add remote Edge model-turn transport`:

- `NewDriver(*Store)` remains compatible and delegates to `NewDriverTransport(ModelTurnTransport)`.
- `ServeDriverTransport` serves the same private Unix-socket driver over a generic transport.
- `RemoteEdgeTransport` obtains its server URL only from the paired identity, rejects redirects, and reuses the existing Ed25519/timestamp/nonce/body-digest signing.
- Persistent local SQLite journal mode is 0600, contains only lease/create/wait identities and bounded temporary large-body staging, and creates no authoritative model-turn/runtime tables.
- `lease_id`, `create_id`, and `wait_id` survive lost HTTP responses and restart; create/wait/cancel and started/completed/failed lifecycle operations are idempotent.
- `result_ref` is immutable after first assignment; changed terminal refs are rejected.
- Large local staged bodies enforce TTL/quota and are deleted after the authoritative VPS body exists.
- Added adversarial tests for lost lease, started/completed/failed/cancel responses and exact ID reuse.
- Fixed a real timeout race so `WaitResponse` returns `ctx.Err()` when SQLite observes cancellation at the transaction boundary, preserving 204/reconnect semantics.

Validated on this exact Step 4 tree:

- `go fmt ./...`
- focused Edge/edgeclient/modelturn/app tests
- timeout/reconnect test repeated 10 times
- `go test ./... -count=1`
- `go vet ./...`
- `go build ./...`
- `git diff --check`

No `tmp_*.go` helper remains. Next action: commit Step 4, update memory with its SHA, then immediately implement Step 5 pinned OpenCode launcher.
