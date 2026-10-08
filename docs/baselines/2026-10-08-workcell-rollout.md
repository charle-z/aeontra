# Sandbox workcell rollout — 2026-10-08

This dated evidence describes the private runner and workcell rollout. Backend
deployment, Edge installation and full BuildKit acceptance are separate facts.

## Published identities

Protected publication run `37726331195` succeeded on
`fcac9ceb0ed2a45e02808e40a6cbc8aa07a4b370`.

| Identity | SHA-256 |
|---|---|
| Workcell index | `fe6574b01c3f2c99aa1460723051d46f2af20b5f425af613e5d2387f5ffde267` |
| Workcell amd64 manifest | `4ecc2ab3e1e0c35b12d0448864ba4dbc7a42f60e09fe825e3c687154c34e9942` |
| Workcell OCI config / Podman image ID | `0708a6f884e4fcc1af57cb25e126629e977d3117a504123b2d80fb2a24772c07` |
| Runner index | `31e4c5553451e3948e86e5a5c9cde1ffb3e996f8d5edf8f66f7f08b8a522a89b` |
| Runner amd64 manifest | `77be550c48f4124aaf45a35f961b5cae7b39c0b79a38e9f79c7d09d65307623d` |
| Dockerfile.sandbox-workcell | `90d871967d9469e07f4b7f231ade595389562e534cb1977d8e73cbadcb443635` |

The release artifact, registry index, amd64 manifest and OCI configuration were
hashed independently. Linux/amd64 and source revision matched. Docker 29's
containerd-backed image store reported the runner index as its local image ID;
the index, descriptor and repository digest were verified rather than treating
that store-specific ID as the OCI configuration digest.

## Host acceptance and activation

Only the two exact workcell digests were added to the rootless policy. Default
reject, previous rules and all other scopes were preserved. Root-private backups
retain the previous policy and runner bindings.

A disposable native Podman API probe passed with UID 10001, no network, read-only
rootfs, all capabilities dropped, no privilege escalation and no host engine
sockets. It reported OpenSSL 3.6.5 and numeric version `0x30600050`.
An initial container-create reply timed out during mapping preparation; its owned
unstarted container was reconciled and removed before one bounded repeat. Only
owned probe containers and files were removed.

Coolify's persisted image variables were updated and the service was restarted
normally. Final verification matched runner and workcell index references and
source revision. The runner was healthy, UID 10001, read-only and nonprivileged;
mounts and resource limits were preserved. No GitHub or registry token entered
the runner or workcell environment.

## Separate gates

- Feature PR: 18 exact-head checks passed. Main, protected publication and signed
  Edge publication passed.
- Both Parrot Edges: signed `v1.3.18`, exact source merge, healthy after update.
- The daily watch must scan this new inventory after it reaches main; source CI
  alone is not evidence of that execution.
- Full Development/BuildKit host acceptance remains outside this image receipt.

The existing accepted npm-bundled http-cache-semantics advisory remains visible
in raw reports and expires at 2026-10-17 13:24 UTC. No exception was extended and
no new fixed/not-affected VEX statement was added.
