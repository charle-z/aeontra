# P16 simple Parrot Edge installation and lifecycle

Status: **Step 2 implementation candidate** on branch `p16-global-work-scheduler`.
Local unit/package/onboarding tests pass. Exact-head remote package, race, signed release,
and real Parrot installation evidence are still required before this workflow is called
merge-ready or deployed.

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

The package now declares `util-linux` because its lifecycle transaction invokes
`runuser`; migration must execute as the Edge user who owns the private state, not as
root.

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

A failed signed update restores the previous release and service. Step 2 adds state
transaction rollback so a package failure cannot leave the old release looking in a
new state path while the previous state disappeared.

A normal update does not require another pairing or manual workspace registration.
This guarantee is local-code/fixture verified but still requires exact-head remote and
real-device evidence before deployment closure.

## Uninstallation posture

Removing the package may remove packaged binaries, units, links, and root-owned
configuration. It must not automatically delete user repositories, lab evidence,
identity, jobs, results, or state under the user's home directory. Destructive cleanup
of private identity/state requires a separate explicit human decision after revocation
and backup review.

## Normal chat experience after installation

P16 Step 2 only fixes installation, migration, onboarding, diagnosis, and repair. The
alias-first project resolver is Step 3. Its final normal interaction will be:

```text
Usa Parrot para continuar ekoparty-trip-agent.
```

The control plane will resolve the alias, device, project, workspace, job, and runtime
internally. Step 2 does not yet claim that this high-level project flow is implemented.

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
