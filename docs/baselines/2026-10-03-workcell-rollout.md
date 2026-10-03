# Sandbox workcell rollout — 2026-10-03

This dated evidence records the sandbox image rollout only. It does not establish
backend deployment, another Edge's installed version or full Development runner
acceptance.

## Published identities

Protected sandbox publication run: `37132982108`, successful on source merge
`f8f641e2d65d696f3ef057c1dda8f99ba5a8cf21`.

| Identity | SHA-256 |
|---|---|
| Workcell index | `a342917779194d6c455f25f760b13d7a1a1f4e88eafec0f231bf099aaa776d41` |
| Workcell amd64 manifest | `bb0691ef833195e08f583bc4787e00cbc0d0bd84f9438a1a76c568a757dbb54f` |
| Workcell OCI config | `0b6e0933cfc57d10fcbfdd805e632c638d2eb1071a5e06e86c4f8526e452a1da` |
| Runner index | `8ea27a9a12a5b212169afcef3dca73205e1a9ed1f7869cee00244c952ec15f0f` |
| Dockerfile.sandbox-workcell | `cc5cb19e289aeb0bf82076da5807b7efcdee56bcb2fd3e1adb7cb74c9fc74ccc` |

The publication artifact, registry index, amd64 manifest and OCI config were
hashed independently. Platform and revision labels matched. The reviewed
workcell uses `py3.14-pip=26.2.1-r2`, its matching base package, Python 3.14 and
vendored urllib3 2.8.0. Pip findings were fixed, not added to the risk exception.

## Host acceptance and activation

The owner approved only the new workcell index and amd64 manifest for the
rootless allowlist. The default reject and all previous rules remained intact;
the superseded candidate's digests were not added. Policy and operative service
configuration were backed up before mutation.

A disposable native Podman API probe passed with UID 10001, network denied,
read-only rootfs, no capabilities and no host engine sockets. It verified the
actual pip/Python/urllib3 combination. Only its owned container and fixture were
removed afterward.

Coolify's persisted runner image variable was updated through its API. The
legacy raw Compose contained a literal old workcell pin; its single field was
changed to consume the already-pinned workcell variable through the supported
service PATCH. The normal service restart regenerated operative configuration.
The first restart revealed that literal binding; activation was not declared
successful from runner health alone.

Final verification: runner and workcell references matched the approved index
digests, runner revision matched the source merge, container was healthy,
mounts were unchanged, UID remained 10001, rootfs remained read-only and
privileged mode remained disabled. GitHub and Coolify tokens were absent from
the runner environment. A private rollback receipt records the exact container,
verification UTC and original configuration without publishing topology or secrets.

## Separate acceptance gates

- Source PR: 18 exact-head checks passed; merge checks and signed image
  publication passed. Dependency review is event-gated on non-PR runs.
- Local Edge: `v1.3.10` installed and healthy on the source merge.
- Other Edge update: queued at this checkpoint; no current-host acceptance
  claim is made for an offline device.
- Daily watch: inventory is `deployed-workcell`; its first manual GitHub scan
  requires separate verification after the inventory change reaches `main`.
- Backend deployment and complete concurrent Development/BuildKit acceptance
  remain separate from this image rollout evidence.

The only accepted vulnerability is the explicitly approved npm-bundled
http-cache-semantics advisory, expiring 2026-10-17 13:24 UTC. It remains visible
in raw reports; this is not a fixed/not-affected VEX claim.
