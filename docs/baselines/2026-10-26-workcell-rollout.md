# Workcell security rollout — 2026-10-26 UTC

The inventory describes the deployed private L3 workcell, not every production image.
Source: `295622828b2a3001464e0e06147ceeacbedd4b20`, merged in
[PR #351](https://github.com/charle-z/aeontra/pull/351).
The protected [image publication](https://github.com/charle-z/aeontra/actions/runs/40248279893)
completed successfully after the exact-head source and seven-image security gates.

| Identity | Verified value |
| --- | --- |
| Index | `sha256:d94a47e1f26098e34d9051d4f2b7cf6c118296f6d040c3722f6f738ec18faa8a` |
| Linux/amd64 manifest | `sha256:a24285f7478ef4bc981a742edbc9fac3fa3453c3e7fb93ba88913054086d89e7` |
| OCI configuration / Podman image ID | `sha256:d6ef8463bdb194429c7fd02fbb34fb43477f12b28967d700d416d55597068c7c` |
| Dockerfile SHA-256 at that source revision | `1291d68a46f1714a0343baa2c6e96499f947c173030a8b28bd39e445fb3977b0` |

Independent registry reads verified the index, manifest, configuration, revision and
platform before pulling the immutable image. Rootless Podman inspection matched the
deployed image ID and source revision. Its default-reject image policy retained every
existing rule and added only the two exact workcell digest scopes.

The private runner's stale directory at the Podman socket path was backed up and removed
only after verifying it was empty. The rootless service and socket were restored;
storage, repositories and existing containers were preserved. Compose now requires an
existing socket and cannot create a directory at that path. The runner is healthy.

Managed backend rollout `2jkzvpug9zebmfhe3bjqwiki` completed at 00:54:30 UTC with journal
state `succeeded`, phase `complete`, revision 705. The real public MCP reports the source
revision above, protocol `2024-11-05`, 198 tools and the unchanged catalog hash.
`sandbox_status` attests the pinned rootless executor with network deny. Real
`sandbox_exec` calls passed `pwd`, clean tracked Git status and Node inspection
(`v24.22.1`, builtin Undici `7.30.1`). Negative checks confirmed read-only rootfs,
absent engine sockets and unreachable external network.

No new risk exception or VEX was introduced. The existing corrected-zlib VEX is
unchanged. A subsequent watch execution is a separate acceptance gate; neither this
inventory nor source CI discovers live production images. No installed Edge was
updated, restarted or reconfigured during this rollout.
