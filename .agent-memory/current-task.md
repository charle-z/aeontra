# P9 Brain production closure and Coolify API autonomy documentation

P9 Brain remains complete / merge-ready from branch `p9-brain`, based on P8 closure `2e3429c9d6342e8e091cadf65293c5c85b1b3259`, and preserves the no resident service invariant. PR #4 was merged as `1faddafd866c426edf5e76d4d336d0b2b7d3f2b6`; production still requires persistent `/brain`, `MCP_DEVBOX_BRAIN_ROOT=/brain`, deployment verification, both smokes, and annotated tag `p9` before console/frontend work.

Current documentation branch: `docs/coolify-storage-autonomy`.

Durable documentation now records:

- inspect the official Coolify API and existing authenticated client before asking the operator to use the panel, SSH, or Docker socket;
- application storages use `/api/v1/applications/{uuid}/storages`;
- P9 Brain storage is the closed persistent definition `mcp-devbox-brain` mounted at `/brain`, with no `host_path`;
- list first, create only if absent, verify by a fresh read, fail on conflicts, and never delete during P9 closure;
- environment variables require create/update semantics (`POST` for missing and supported `PATCH` for existing); a `409` is not a token-permission failure;
- only `401`/`403` justify asking for authentication or the exact missing token ability;
- empty `COOLIFY_ALLOWED_APPS` intentionally permits management of structurally valid application UUIDs on the configured instance;
- storage automation does not require a public Docker socket, SSH, free shell, `MCP_DEVBOX_MODE=allow`, a new application, or privileged profiles when a narrow API workflow is available.

Verification completed: `go test ./docs -count=1`, `go test ./... -count=1`, and `git diff --check` passed. Next: commit and publish this independent documentation branch. The overnight P9 implementation remains separate. Do not start Opus/Fable console work before P9 is deployed, verified, and tagged.
