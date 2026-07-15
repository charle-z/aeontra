# Current task

P8.1 Console 2.0 is deployed from merge `d343264bffdc0ae1bc045a9d723e913be977090c`. P9 Brain is deployed underneath the current P11 production baseline. This branch changes neither production closure.

P11.1 post-merge production verification completed on 2026-07-15.

- PR #12 merged from head `00857da8f26f8130f2eab6115ebeb2b56e5ea8ce`.
- Merge commit and fetched `origin/main`: `01fde5067752ab1c43424d2d54f9afd914617ba5`.
- Existing Coolify application `jqf7qz5ensoqtvl1tb197gcv` remains `running:healthy`; production smokes report the merge commit, 77 tools and catalog hash `sha256:3f4e1812bd72a0508eba108d97dfd353ea9abc4c883cded262abd768f1f94518`.
- No manual deployment was triggered. The loaded connector does not expose `mcp_client_capabilities`, so sampling remains undetermined; `PullRendezvousTransport` remains active and `MCPSamplingTransport` inactive.

P11.2 branch: `p11-2-remote-opencode-relay`, based exactly on merged `origin/main` commit `01fde5067752ab1c43424d2d54f9afd914617ba5`.

Completed:

- Step 1 commit `97f9956`: authoritative model runtimes now bind opaque Edge device/workspace IDs, distributed states, immutable goal refs/digests, heartbeat, expiry, idempotency, active turn metadata and optional result refs while preserving legacy local runtimes.
- Step 2 implementation is locally green: private local SQLite workspace registry, opaque generated workspace IDs, add/list/remove CLI commands, absolute Linux paths, owner validation, symlink rejection, no Windows mounts and revalidation before each resolution. No workspace-management tool is exposed over MCP.

Next: commit Step 2, then expose the signed device-bound model runtime and model-turn Edge relay endpoints. Do not merge, deploy, tag, pair a real device, install on Parrot or modify Coolify.
