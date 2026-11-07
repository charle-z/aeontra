# Workcell security rollout — 2026-11-07 UTC

This baseline covers the deployed private L3 workcell. Other Coolify components and
installed Edge bundles require separate deployment evidence.

Source: `4ef3d3360ffd0d6334e7fec7c5426f469d43398a`, merged in
[PR #355](https://github.com/charle-z/aeontra/pull/355), whose exact source head passed
all 18 checks. The protected [image publication](https://github.com/charle-z/aeontra/actions/runs/42351150246)
verified and promoted the same immutable artifacts after their security gates.

| Identity | Verified value |
| --- | --- |
| Index | `sha256:efa34e7c57550126e6e448dceb7a427a724aaf1ee038a39aaaac47fca2049d70` |
| Linux/amd64 manifest | `sha256:3bc51e01b280e0191b4c20dbd5d28077771ddb7fc95a5a513c5dce2b945cb3f3` |
| OCI configuration / Podman image ID | `sha256:f873e7ce76604e02eb42615ebf013184f1e309306672621362bf6f93765c9764` |
| Source Dockerfile SHA-256 | `8e7beba632c9c6fbaaa8e697300fa4c6167ac759bf7e6a3559f19bd48de365db` |

The dedicated rootless engine matched the protected artifact's configuration ID and
both registry digests. Its default-reject policy retained every existing rule and
added only the two exact workcell digest scopes. The private runner was recreated
with its protected immutable image and became healthy; repository, receipt-state and
rootless-socket mounts remained unchanged. No storage or repository was removed.

Real-host containment checks passed with UID 10001, `keep-id`, no network, a read-only
rootfs, dropped capabilities and no new privileges. After activating the runner,
real `sandbox_status` and selected-workspace `sandbox_exec` calls passed. The MCP
execution verified `/workspace`, non-root identity, read-only rootfs, absent host
home and engine sockets, denied external egress, Expat 2.10.0, Node 24.22.1 and
builtin Undici 7.30.1. Runtime credentials were not supplied to the workcell.

The archived protected-release proof verifies 794 installed libc files, the traversal
regressions and normal translation controls, and a complete 18,504-file inventory
with the official and renamed SQLite CLI positive controls. The High gate qualifies
only the corrected glibc finding and the absent SQLite CLI component for this exact
image. Raw findings remain retained; unrelated High/Critical findings still block.

The proof uses the unchanged runtime policy expiring on 2026-11-21. The separate
daily retirement workflow checks official replacements and does not renew the policy
or deploy. The existing corrected-zlib VEX is unchanged; no new risk exception is
introduced. A successful security-watch run on this updated inventory is separate
acceptance evidence. No installed Edge was updated, restarted or reconfigured.
