# P15 one-time Parrot installation and onboarding

Status: P15 release-candidate workflow. Do not apply it to the real Parrot device
until exact-head CI, package tests, merge, automatic deployment and the signed release
publication are complete.

## Operator workflow

The supported initial artifact is the official `mcp-devbox-edge_<version>_amd64.deb`
plus its detached armored signature and SHA-256 file. Release automation publishes
all three from the same immutable commit. The package must be obtained from the
official release and its signature/hash must be verified by the supported bootstrap
or signed APT repository before installation.

The only privileged installation action is:

```text
sudo apt install ./mcp-devbox-edge_<version>_amd64.deb
```

Then the future Edge user performs one guided action:

```text
mcp-edge onboard --server https://mcp-devbox-charlez.duckdns.org
```

The pairing code is read from standard input and is never accepted as an argument.
Onboarding verifies the installed signed bundle, runs the Bubblewrap/systemd/rootless/
Node/Go/OpenCode/provider/driver preflight, pairs without replacing an existing
identity, and waits for the systemd path unit to start and health-check the Edge. It
prints one safe final result containing only the device ID and valid/active states.

After this bootstrap, the operator does not compile, copy binaries/providers/drivers,
edit units, restart services or register workspaces for individual machines.

## Installed layout

The Debian package installs one release below `/opt/mcp-devbox/releases/<RELEASE>`,
atomically points `/opt/mcp-devbox/current` to it, and maintains only these compatibility
links:

- `/usr/local/bin/mcp-edge`;
- `/usr/local/libexec/mcp-devbox/model-turn-driver`;
- `/usr/local/libexec/mcp-devbox/mcp-autopilot-worker`;
- `/usr/local/libexec/mcp-devbox/mcp-bundle-updater`;
- `/opt/mcp-devbox/opencode-provider`;
- `/opt/mcp-devbox/opencode-1.18.1`.

It installs the Edge template, restricted updater oneshot and onboarding path unit
under `/etc/systemd/system`, root-owned configuration under `/etc/mcp-devbox`, and
documentation under `/usr/share/doc/mcp-devbox`.

## Migration and repeat installation

Package installation is idempotent. It reuses the same release and links, never
regenerates identity or keys, and never deletes workspaces, contracts, checkpoints or
artifacts. The current P12–P14 state root
`~/.local/state/mcp-edge` is left in place. Only when that preferred identity is absent
and the legacy `~/.config/mcp-devbox-edge` identity exists is the complete private state
directory moved atomically to the preferred location; bytes and opaque IDs are not
rewritten.

`postinst` records the previous release link, activates the new link atomically,
installs the signed unit, runs bundle verification and preflight, and restores the
previous link/unit if any step or final service health check fails.

## Automatic updates and repair

`mcp-bundle-updater` runs only as root in a hardened oneshot. Its accepted operations
are exactly `status`, `update stable`, `rollback`, and `repair`; no URL, path, command,
script or caller-provided hash is accepted. `stable` is resolved only from the compiled
official GitHub release base. The Ed25519-signed canonical channel binds release,
commit, protocol, catalog, architecture and archive hash.

The updater downloads a bounded archive, rejects unexpected entries, traversal,
links, duplicates and oversized files, verifies every bundle component in staging,
renames the release into place, swaps `current`, installs only the packaged Edge unit,
restarts only the configured Edge service, checks health and restores the previous
signed release on failure. Rollback accepts only the prior locally known signed bundle.
Repair restores exact compatibility links/modes/unit/service from a valid signed
release or fetches `stable` when the active bundle is incomplete. Cleanup always keeps
current, previous and at least one additional signed release, and removes only older
signed P15 directories after 30 days.

The unprivileged Edge can request only three fixed root-owned units through a generated
polkit rule: official stable update, previous signed rollback, and official repair.
The rule accepts only `start` for those exact unit names. A private `0600` operation
receipt survives the Edge restart performed by an update; the same operation resumes
diagnosis, while a different or malformed receipt fails closed. The receipt is removed
only after the signed control plane acknowledges completion.

Public tools never accept updater implementation details. `edge_bundle_update` accepts
only `device_id` plus `release=stable`; status, rollback, repair and onboarding status
accept only `device_id`. Diagnostics contain opaque identity and version/health booleans
plus closed blocker codes—never URLs, filesystem paths, hashes supplied by a caller,
commands, scripts, targets, credentials or flags.
