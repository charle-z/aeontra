# Canonical Parrot Edge installation and lifecycle

This is the **Canonical** operator procedure for the current source tree. General
runtime variables, paths, ownership, and security posture remain canonical in
[`configuration.md`](configuration.md) and [`security.md`](security.md).

Keep four states separate: source release, package artifact, VPS/control-plane
deployment, and installed Edge. Each requires separate evidence. When any environment-
specific proof is missing, report that proof as **validation pending** rather than
inferring it from source, CI, a tag, or another device.

This guide does not identify a moving release or claim which release is installed on a
real Edge. Use the signed package metadata and supported local doctor/status output.

## Product contract

The normal Edge lifecycle must not require the operator to copy binaries, edit systemd
units, rename P12/P15 folders, recreate a workspace, or carry `ed_*`, `ws_*`, `mr_*`,
job, lease, or idempotency identifiers between chats.

A clean Edge requires at most these two local commands:

```text
sudo apt install ./mcp-devbox-edge_<version>_amd64.deb
mcp-edge onboard --server https://mcp-devbox-charlez.duckdns.org
```

The first pairing still needs one short-lived code authorized by the control plane. The
code is read from standard input and never accepted as a command-line argument. This is
a one-time trust bootstrap, not a per-project or per-chat setup step.

After a valid identity exists, running `mcp-edge onboard` again:

- loads and verifies the existing identity/private key;
- does not read a pairing code;
- does not contact the pairing endpoint;
- optionally verifies that a supplied server matches the stored server;
- waits for the fixed Edge service to become active;
- prints only the human alias and safe health states.

Example safe result:

```text
onboarding complete alias=parrot service=active bundle=valid pairing=reused
```

The output deliberately omits the opaque device ID.

## What the package installs

The signed Debian package installs one immutable release under:

```text
/opt/mcp-devbox/releases/<SIGNED_RELEASE>
```

and atomically points:

```text
/opt/mcp-devbox/current
```

to that release. Compatibility links, the fixed systemd units, root-owned updater,
polkit rule, reviewed Node/OpenCode/provider/driver components, and the rootless Podman
prerequisites remain governed by the signed P15 bundle contract.

The package declares `util-linux` because its lifecycle transaction invokes `runuser`;
migration must execute as the Edge user who owns the private state, not as root. It also
declares the official GitHub CLI package `gh`: this supports both the operator's normal
interactive `gh auth login` and the separate direct-Edge broker import documented in
[`development-edge-git.md`](development-edge-git.md).

Archive-only updates do not invoke APT. The signed manifest-v3 layout owns a
pinned official `gh` at `libexec/gh` and a managed `/usr/local/bin/gh` compatibility
link. A manifest-v2 bridge release must be installed first on older devices so their
updater can verify v3. Rollback to a v1/v2 release removes only that exact managed link
and preserves any unrelated system installation.

During package configuration and archive activation, the privileged lifecycle inspects exactly
`mcp-devbox-edge.service` and `mcp-devbox-opencode-edge.service`. If either known
legacy unit is loaded, it is stopped and disabled before the current templated unit is
restarted. No caller-controlled service name is accepted. This prevents an older
orphaned Edge process from retaining the state lock while preserving fail-closed
single-process behavior.

## Existing P12/P15 state

Canonical private state:

```text
~/.local/state/mcp-edge
```

Legacy state candidate:

```text
~/.config/mcp-devbox-edge
```

An arbitrary directory named `p12` is not assumed to be MCP Devbox state. It is
classified read-only and left untouched unless an exact reviewed legacy rule matches.

On package configuration, the fixed lifecycle performs:

```text
recover
-> prepare migration
-> activate/restart and health-check signed service
-> finalize
```

The only automatic state move is the complete legacy directory to the canonical state
root. It requires a valid identity/private key, private modes, correct Edge-user
ownership, exact canonical paths, no destination, no conflicting identity, and no
unsafe symlink.

The package does not rewrite identity, keys, workspace IDs, contracts, checkpoints, or
pending results. The remote package fixture compares identity, key, workspace database,
and checkpoint byte for byte.

## Transactional rollback

The package retains a private verified migration journal until the new service passes
health. If bundle verification, preflight, service activation, or final health fails,
the package trap:

1. atomically moves the state back to the legacy path;
2. verifies identity and ownership again;
3. removes/fsyncs the journal;
4. restores the previous signed release/unit transaction.

A hard interruption is recovered on the next package/doctor run from the same journal.
It never guesses when source and destination are ambiguous.

The exact-head remote package workflow must prove both cases in a clean Debian
container:

- forced service-health failure restores legacy state byte for byte;
- the following successful run migrates once and a repeat `postinst` is idempotent.

Until that remote gate is green, this package candidate remains validation pending.

## Diagnosis

Read-only diagnosis:

```text
mcp-edge doctor
```

The command checks:

- installed signed bundle;
- state/workspace/release/systemd layout blockers;
- existing identity validity;
- user-owned rootless Podman or Docker endpoint;
- the fixed per-user Edge service.

It returns a bounded line such as:

```text
edge_doctor status=ready bundle=valid layout=valid identity=valid alias=parrot service=active rootless=podman
```

Possible high-level states include:

```text
ready
setup_required
degraded
blocked
```

It never prints filesystem paths, symlink targets, keys, commands, repository content,
or opaque device/workspace/runtime IDs.

## Closed repair

Repair is requested with:

```text
mcp-edge doctor --repair
```

The command accepts no other path, URL, command, script, hash, service, or executable.
It may only:

1. recover or complete the fixed state migration;
2. run the exact reviewed legacy-to-canonical migration when eligible;
3. start the fixed root-owned `mcp-devbox-edge-repair.service`;
4. re-run safe health checks.

The privileged repair unit restores only the active/previous signed bundle,
compatibility links, packaged unit, expected modes, and configured Edge service. It
does not modify repositories, branches, project files, workspaces, or arbitrary host
paths.

## Updates

Normal signed bundle updates continue through the fixed updater/polkit path. They
preserve:

- Edge identity and alias;
- project/workspace registrations;
- local contracts and checkpoints;
- pending jobs/results;
- configured resource policy;
- repositories below `~/workspaces` and `~/htb-machines`.

A failed signed update restores the previous release and service. The lifecycle
transaction also restores migrated state so a package failure cannot leave the old
release pointing at a state path whose prior contents disappeared.

A normal update does not require another pairing or manual workspace registration.
Verify this separately in package CI and on the intended real device. Do not transfer
proof from one environment to another.

## Uninstallation posture

Removing the package may remove packaged binaries, units, links, and root-owned
configuration. It must not automatically delete user repositories, lab evidence,
identity, jobs, results, or state under the user's home directory. Destructive cleanup
of private identity/state requires a separate explicit human decision after revocation
and backup review.

## Normal chat experience after installation

After installation, a registered development project may be addressed by its human
project and Edge aliases rather than by opaque device/workspace/runtime identifiers.
For example:

```text
Usa Parrot para continuar ekoparty-trip-agent.
```

The control plane and Edge resolve private identifiers through their registered
contracts. Verify readiness with the project/status tools and local doctor output; the
example itself is not evidence that a particular project or device is ready.

## Validation commands for maintainers

```text
go test ./cmd/mcp-edge ./internal/edgelifecycle ./internal/edgeclient ./packaging/debian -count=1
go vet ./cmd/mcp-edge ./internal/edgelifecycle ./internal/edgeclient ./packaging/debian
go test ./internal/edge ./internal/edgeclient ./internal/edgelifecycle ./internal/bundle ./internal/edgeupdate ./cmd/mcp-edge ./cmd/mcp-bundle-updater ./packaging/debian ./packaging/parrot -count=1
go test ./docs -count=1
git diff --check
```

The remote `.github/workflows/p15-edge.yml` package transaction and CGO race gates are
blocking; local success is not a substitute.

## Durable job journal

The development workcell persists execution state in the private `journal.db`. `mcp-edge doctor` reports `journal=empty|ready|pending|reconciliation|migration_required|blocked` without exposing task or result identifiers. A transient disconnect may continue locally for the fixed ten-minute offline grace. Completed pending results replay after reconnection without re-execution. Do not delete or edit `journal.db`; use the local `STOP` file before manual reconciliation. Delivered results become eligible for bounded cleanup after seven days, while pending evidence is retained. See `docs/edge-job-journal.md`.
