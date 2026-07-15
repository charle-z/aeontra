# Current task

P11.1 post-merge production verification completed on 2026-07-15.

- PR #12 merged from head `00857da8f26f8130f2eab6115ebeb2b56e5ea8ce`.
- Merge commit and fetched `origin/main`: `01fde5067752ab1c43424d2d54f9afd914617ba5`.
- Existing Coolify application `jqf7qz5ensoqtvl1tb197gcv` remains `running:healthy`; production smokes report the merge commit, 77 tools and catalog hash `sha256:3f4e1812bd72a0508eba108d97dfd353ea9abc4c883cded262abd768f1f94518`.
- No manual deployment was triggered. The loaded connector does not expose `mcp_client_capabilities`, so sampling remains undetermined; `PullRendezvousTransport` remains active and `MCPSamplingTransport` inactive.

P11.2 branch: `p11-2-remote-opencode-relay`, based exactly on merged `origin/main` commit `01fde5067752ab1c43424d2d54f9afd914617ba5`.

Step 1 implemented and locally green:

- authoritative SQLite runtime rows bind opaque `runtime_id`, `device_id` and `workspace_id`;
- additive migration preserves P11.1 local runtimes and legacy `status` while adding distributed `state`;
- states cover requested, awaiting_edge, starting, awaiting_model, executing_tools, completed, failed, cancelled, disconnected and expired;
- controller is explicit and remains pull-rendezvous for local P11.1 runtimes;
- remote goals are stored only in immutable bounded bodies addressed by opaque refs and SHA-256 digests;
- metadata contains only a content-free goal digest summary, not prompts, paths, commands, arguments, IPs or secrets;
- device-bound leasing, heartbeat, expiry, result refs, active sequence/turn and idempotency are enforced;
- wrong-device reads, digest mismatch and idempotency conflicts fail closed;
- legacy-schema migration and all repository tests pass.

Next: commit Step 1, then implement the private local Edge workspace registry and human-only `mcp-edge workspace add|list|remove` commands. Do not merge, deploy, tag, pair a real device, install on Parrot or modify Coolify.
