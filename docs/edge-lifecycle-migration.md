# Edge lifecycle inventory and state migration

Status: **P16 Step 1 implemented library and local closed CLI; installer integration is
pending Step 2**.

Package automation remains pending Step 2.

This document is the repository truth for discovering and migrating historical P12/P15
Edge state. The Brain may index this page but does not replace it.

## User-facing objective

Normal installation and update must not require the operator to find, rename, copy, or
re-register Edge state manually. After Step 2, the signed package/onboarding flow will
invoke the closed lifecycle operations automatically.

The current Step 1 local commands exist for package integration, tests, and emergency
local diagnosis:

```text
mcp-edge lifecycle inspect
mcp-edge lifecycle migrate-state
mcp-edge lifecycle recover-state
```

They accept no paths, commands, URLs, IDs, or free-form parameters. They are not the
intended routine chat workflow.

## Canonical and historical paths

Canonical state:

```text
~/.local/state/mcp-edge
```

Historical P12/P15 state candidate:

```text
~/.config/mcp-devbox-edge
```

Canonical project roots:

```text
~/workspaces
~/htb-machines
```

Signed installation root:

```text
/opt/mcp-devbox/releases/<release>
/opt/mcp-devbox/current -> releases/<release>
```

A directory named `p12` is not inherently MCP Devbox state. The read-only inventory
may classify `~/p12` as a repository, signed release, Edge state, unknown directory,
file, symlink, or missing path. Unknown content is never moved, renamed, repaired, or
deleted.

## Read-only inventory

`internal/edgelifecycle.InspectLayout` uses `Lstat` and fixed markers. It does not create
any directory or file.

It reports typed status for:

- preferred and legacy state;
- development and HTB roots;
- the active signed release link;
- explicitly configured historical candidates.

The packaged local inventory also checks only this fixed unit-name set under
`/etc/systemd/system`; it never enumerates or modifies unrelated services:

```text
mcp-devbox-edge.service
mcp-devbox-opencode-edge.service
mcp-devbox-opencode-edge@.service
mcp-devbox-edge-onboard@.path
mcp-devbox-edge-repair.service
```

This distinguishes historical P12 units from the current P15 template before Step 2
performs any package cleanup or activation.

Stable blockers include:

```text
preferred_state_symlink
legacy_state_symlink
preferred_state_ancestor_symlink
legacy_state_ancestor_symlink
preferred_identity_marker_symlink
legacy_identity_marker_symlink
preferred_state_occupied
state_identity_conflict
development_root_symlink
development_root_ancestor_symlink
lab_root_symlink
lab_root_ancestor_symlink
development_root_not_directory
lab_root_not_directory
current_release_not_symlink
current_release_ancestor_symlink
current_release_outside_root
current_release_target_absent
systemd_root_symlink
systemd_root_not_directory
```

The local CLI renders only kinds, migration disposition, and blocker codes. It omits
raw paths, symlink targets, identity/device IDs, workspace IDs, and file contents.

## Migration eligibility

The only Step 1 migration is:

```text
~/.config/mcp-devbox-edge
  -> ~/.local/state/mcp-edge
```

It is eligible only when:

- legacy state contains a valid MCP Edge identity and private device key;
- the root, identity, and key have private permissions;
- the root, identity, and key belong to the configured Edge user/group;
- preferred state is absent;
- no conflicting identity, occupied destination, unsafe symlink, or stale journal
  exists;
- migration executes as the Edge state owner, not as a different/root process;
- all canonical paths still match the reviewed plan at execution time.

A valid preferred identity means no migration is needed. Two identities or an occupied
preferred directory block automatic migration and require an explicit diagnosis.

## Versioned journal and atomic move

Step 1 uses journal schema version `1` at:

```text
~/.local/state/.mcp-edge-state-migration-v1.json
```

The journal is a private regular file owned by the Edge user. It stores only:

- schema version;
- fixed migration kind;
- canonical source and destination;
- stage;
- creation/update timestamps.

Stages:

```text
prepared -> renamed -> verified
```

Execution order:

1. Re-inspect and compare the exact plan.
2. Verify the process is the Edge state owner.
3. Create/validate the private destination parent.
4. Persist and fsync `prepared`.
5. Move the entire directory with Linux `renameat2(..., RENAME_NOREPLACE)`.
6. Persist and fsync `renamed`.
7. Load and validate the migrated identity/key at the destination.
8. Confirm the legacy source is absent.
9. Persist and fsync `verified`.
10. Remove/fsync the journal and report success.

`RENAME_NOREPLACE` prevents an appeared destination from being overwritten. A
cross-filesystem move fails securely; Step 1 does not copy state file by file.

## Failure and rollback

Before the verified commit point, any ordinary error triggers rollback:

- when no rename occurred, remove the journal and leave the source untouched;
- when the rename occurred, atomically move the destination back to the legacy source;
- revalidate identity and ownership;
- remove/fsync the journal.

If both source and destination exist, both are missing, a path becomes a symlink, or
rollback cannot be proven, the journal is retained and the operation stops with a
stable error. It never guesses which copy is authoritative.

Failure-injection tests cover errors after journal creation, after rename, and after
verification. State bytes, identity, workspace database fixtures, and checkpoints are
preserved.

## Crash recovery

`mcp-edge lifecycle recover-state` is idempotent:

- no journal: `not_needed`;
- source exists and destination does not: validate source, remove stale journal,
  report `recovered_rollback`;
- destination exists and source does not: validate destination, remove journal,
  report `recovered_complete`;
- any ambiguous or unsafe combination: stop and preserve evidence.

A crash after the atomic move but before updating the journal is therefore recoverable
without performing a second migration or regenerating identity.

## Stable error codes

Migration errors expose a bounded code, not raw paths or secret-bearing underlying
messages:

```text
inventory_blocked
state_invalid
owner_mismatch
wrong_executor
plan_changed
journal_exists
journal_invalid
prepare_failed
rename_failed
verification_failed
rollback_failed
recovery_ambiguous
```

## What Step 1 does not yet do

- It does not change `packaging/debian/postinst.in`.
- It does not run against the real Parrot Edge.
- It does not migrate arbitrary directories or repositories.
- It does not repair permissions automatically.
- It does not skip pairing in `mcp-edge onboard` yet.
- It does not discover/create project aliases or workspaces yet.
- It does not install the P16 scheduler or worker.

Step 2 must replace the historical direct shell `mv` in `postinst` with the closed
recover/plan/apply lifecycle, invoke it as the Edge user, reuse a valid identity, and
prove package rollback from representative P12/P15 fixtures.

## Verification

Local Step 1 checks include:

```text
go test ./internal/edgelifecycle -count=1
go vet ./internal/edgelifecycle
go test ./cmd/mcp-edge -count=1
```

The race test requires CGO and remains a blocking remote CI gate when the current local
MCP execution environment has CGO disabled.
