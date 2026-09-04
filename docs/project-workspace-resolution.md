# P16 project aliases and workspace resolution

Status: **Development-environment v2 is implemented on the current feature branch.
The registry, checkout diagnostics, durable process binding, runtime-root separation,
bounded Edge concurrency, and toolbox migrations still require exact-head CI and
real-device acceptance before release.**

This document is the repository source of truth for the durable project registry and
its local read-only alias interface. The Brain may index it but does not replace it.

## Purpose

A normal project lookup uses a human alias rather than copied device, workspace,
runtime, job or idempotency identifiers.

The local diagnostic interface is:

```text
mcp-edge project discover --alias ekoparty --repository ekoparty-trip-agent
mcp-edge project status --alias ekoparty [--target parrot]
mcp-edge project resolve --alias ekoparty [--target parrot]
```

The normal chat-facing interface is:

```text
project_prepare(alias=ekoparty, repository=ekoparty-trip-agent, target=parrot)
project_status(alias=ekoparty, target=parrot)
```

These public tools accept human names only. They do not accept or return device,
workspace, operation, runtime, job or plan IDs. The server resolves `parrot` to exactly
one active paired Edge; a missing or duplicate active alias blocks rather than choosing
silently. The consequential `project_prepare` tool is the approval boundary. Its Edge
implementation still creates a short-lived internal plan, re-runs discovery and applies
only the reviewed action, but the user never handles a plan ID.

These commands accept no state path, workspace ID, device ID, runtime ID, command, URL
or repository mutation. `discover` scans only bounded direct children of the approved
development root and returns a safe recovery decision without a candidate path. `status`
reports a safe ready/blocked result. `resolve` returns the same safe result but fails when
the project cannot be safely resolved.

The normal JSON result contains only:

```text
alias
owner/repository
target alias
ready, dirty or blocked state
workspace profile and mode when ready or dirty
stable blocker reason when blocked
bounded diagnostic reason and recovery hint when available
```

It never contains a local path or opaque internal identifier.

## Private registry

The project database is a private regular file:

```text
~/.local/state/mcp-edge/projects.db
```

It is separate from:

```text
workspaces.db
jobs.db
```

The current schema version is `2`; schema `1` records are migrated in place and a
newer schema is rejected. SQLite is configured with one connection, full
synchronous writes, foreign keys, a five-second busy timeout and a bounded page count.
The file must be owned by the Edge user, must not be a symlink and must not grant group
or world access. A schema newer than the packaged reader fails closed.

Tables:

```text
projects
project_profiles
project_workspaces
```

The human alias is the project primary key. There is intentionally no user-facing
opaque project ID. `(owner, repository)` is unique so the same repository cannot be
silently assigned competing aliases.

A workspace ID remains an internal binding only. Resolution always loads it through
the existing workspace registry, which revalidates ownership, mode, symlink ancestry,
approved Linux roots and Windows-mount rejection before use.

## Alias and repository identity

Project aliases:

- are trimmed and ASCII case-folded to lowercase;
- contain only lowercase letters, digits and internal hyphens;
- are one to 63 characters;
- cannot start or end with a hyphen;
- reject traversal, underscores, Unicode and visually confusable characters.

`Ekoparty` and `ekoparty` therefore resolve to one canonical alias and cannot create two
records.

Repository identity is canonicalized as lowercase `owner/name`. The owner must equal
the locally configured GitHub owner authority. Repository names use the existing closed
Git development grammar and reject paths, separators, Unicode and leading dots.

An alias grants no new authority. It can select only an already allowed owner,
repository, target alias, profile and registered workspace.

## Registration and idempotence

The Step 3 core exposes typed registration and preparation operations. There is no
public or local free-form `project register` command. `project_prepare` can only
reuse the canonical checkout, associate one unique safe legacy path, or clone the
owner-bound repository into its inferred canonical direct child. The action is selected
and revalidated locally by the Edge, not supplied by the caller.

Initial registration requires:

- one canonical alias;
- one owner-bound repository identity;
- one preferred human target alias;
- one already registered workspace binding;
- one to four allowed workspace profiles;
- a safe checkout whose local Git identity matches the project.

Repeating the exact registration is idempotent. Reusing an alias for a different
repository, reusing a repository under another alias, changing the target/workspace, or
binding a disallowed profile fails with a stable code.

## Checkout revalidation

Registration and full checkout resolution revalidate the physical checkout. Registry-only
lookups used by process observation and cleanup do not run Git status or discovery, but
they still validate the registered workspace boundary and its durable attestation. A
legacy registry row without an attestation is reported as repairable and must pass
`project_reconcile` before registry-only operations can use it. The default Linux
inspector runs only fixed, read-only Git operations with no shell:

```text
git rev-parse --show-toplevel
git remote get-url origin
git remote get-url --push origin
git status --porcelain=v1 --untracked-files=normal
```

Host and global Git configuration are disabled, hooks are disabled, file-protocol
access is denied, terminal prompting is disabled, execution is time-bounded and output
is bounded.

A checkout identity is valid only when:

- the registered workspace remains safe and below an approved root;
- `.git` is a real directory, not a symlink;
- the top-level checkout equals the registered workspace path;
- fetch and push remotes are owner-bound HTTPS GitHub URLs for the exact repository;
- the repository inspection succeeds. The resulting project state is `ready` when the
  tree is clean and `dirty` when it contains normal tracked or untracked changes. A
  dirty tree remains an authorized development state; it is not a security failure.
  Registration, fast-forward and publication retain their explicit clean-tree
  preconditions and return `checkout_dirty` when they cannot safely proceed.

The Edge does not provision new toolchains or caches in the source tree. Each workspace
has private roots under the Edge state root:

```text
<edge-state>/project-runtime/<workspace-id>/
<edge-state>/project-cache/<workspace-id>/
<edge-state>/project-artifacts/<workspace-id>/
```

Linux workcells and rootless toolboxes mount only the exact source, runtime, cache and
artifact roots. `HOME`, `CARGO_HOME`, `RUSTUP_HOME`, package-manager caches and language
module caches point to those private roots. Existing `.mcp-devbox` directories remain
user data and are not removed automatically. A legacy workcell may have generated
`runtime/`, `cache/`, `tools/`, `browser-harness/`, `authorization-revision`,
`instructions.md`, `current-state.md`, `lab-contract.json`, or
`tool-inventory.json` below that directory; those exact legacy
surfaces remain recognized for compatibility. New Linux workcells place their control
files under the private runtime root and expose them inside the sandbox as
`/workspace/.mcp-devbox`; arbitrary files below `.mcp-devbox` are ordinary project data
and make the checkout dirty.

The native Windows Edge derives the same three private roots from its configured state
root on any supported fixed local drive. It does not assume `C:` or a particular
workspace location. ACL, reparse-point, ownership and containment checks apply to every
root before a workcell or process is started.

The registry stores a physical workspace attestation. It detects a workspace or
repository replacement without hashing mutable source contents or using directory mtime
as identity. A changed attestation is `identity_mismatch`; ownership, containment,
symlink and mount violations are `unsafe_boundary`.

Stable resolution blockers include:

```text
invalid_input
owner_denied
alias_conflict
repository_conflict
profile_denied
project_not_found
target_not_found
workspace_unavailable
workspace_conflict
checkout_dirty
checkout_missing
ambiguous_checkout
repository_mismatch
checkout_unsafe
discovery_limit
discovery_timeout
plan_changed
plan_expired
registry_unavailable
workspace_registration_failed
workspace_validation_failed
workspace_lookup_failed
workspace_write_failed
project_registration_failed
credential_unavailable
clone_failed
cleanup_required
```

Errors include a bounded stable reason, repairability and recommended action when the
public tool contract allows diagnostics. They do not include paths, remote URLs, Git
output, credentials or internal IDs. Detailed physical values remain private to the
Edge recovery path.

## Discovery and recovery decisions

Normal resolution is registry-first: it loads the durable project/workspace binding,
validates the workspace boundary and attestation, and only runs Git inspection when the
caller needs current checkout state. Process status, process stop, process list and
other registry-only operations do not perform filesystem discovery or Git status.

Recovery discovery validates the development root and reads at most 128 direct children
by default, with a hard maximum of 512 and one 30-second total deadline. It never
recurses, follows a symlink, creates a directory, registers a workspace or changes Git
state. Slow or failed inspection is reported as `checkout_timeout` or
`checkout_unavailable`, not as a generic unsafe state.

The canonical checkout has priority. If it exists but is unsafe, dirty or points to a
different repository, discovery blocks instead of silently choosing another directory.
When the canonical path is absent, only checkouts whose fetch/push identity matches the
expected owner/repository count as candidates.

Stable recovery states are:

```text
reuse_existing
associate_existing
clone_required
blocked
```

One clean legacy match yields `associate_existing`. More than one matching checkout yields
`ambiguous_checkout`; no path is guessed. A dirty unique match is visible as dirty and
remains ineligible for operations with an explicit clean-tree precondition. No match
yields `clone_required`. An approved `project_prepare` may then clone only the
canonical owner-bound repository using the Edge credential already configured for
development Git.

The safe discovery JSON contains alias, owner/repository, recovery state, candidate count
and a stable reason. It omits all candidate paths and internal identifiers.

## Preparation preview/apply transaction

The core exposes an internal typed preview/apply operation for a clean canonical checkout,
one unique legacy checkout or one missing canonical checkout. It can associate a unique
safe legacy path without moving it or clone a missing project after public tool approval.
The safe preview reports `approval_required`, alias, owner/repository, target, profile
and action; the reviewed plan keeps the candidate path internal and expires after five
minutes. There is intentionally no user-facing plan ID.

Apply re-runs discovery and requires the same action and exact candidate. A new match, dirty
checkout, remote change, symlink replacement, root escape or expired plan returns
`plan_changed` or `plan_expired` before mutation.

For reuse/association, apply registers the existing path in `workspaces.db` and binds
the project. It never renames, copies or deletes an existing checkout. If project binding
fails after a new workspace registration, that registration is removed; the checkout
remains untouched. An already registered compatible workspace is reused. One workspace
cannot be bound silently to two projects.

For clone, the Edge creates the canonical directory exclusively with mode `0700` and
holds an open directory descriptor for the whole operation. The fixed Git runner receives
only the generated owner-bound HTTPS URL and executes `git clone --single-branch -- URL .`.
It uses the existing private Edge GitHub credential, disables prompts, hooks, system Git
configuration and file-protocol access, and bounds time/output. After clone, the normal
checkout inspector revalidates root, `.git`, fetch/push remotes and cleanliness before
any registry mutation.

A normal clone failure removes only the exact reserved directory. If the path or inode was
replaced, cleanup stops with `cleanup_required` and preserves the replacement for review.
A crash after a successful clone but before registration converges on the next discovery as
`reuse_existing`; it does not blindly clone again.

## Canonical checkout inference

The pure inference helper maps repository `charle-z/ekoparty-trip-agent` to checkout
name `ekoparty-trip-agent` below the configured development root. It validates that the
result is one direct child and does not create or modify the filesystem.

Inferring a path is not authority to create it. Discovery and preparation use separate
read-only and consequential phases. Only approved `project_prepare`, after local
revalidation and credential checks, can reserve and populate the canonical path.

## Persistence and rollback

Alias, repository, target, profile, claim generation and attestation metadata survive
close/reopen. The registry is additive: existing low-level workspace tools and
`workspaces.db` retain their current behavior. Registry-only claim listing and
reconciliation can identify a stale claim without scanning unrelated repositories;
healthy claims are never released implicitly. Release/reassociation is bound to the
recorded repository identity and increments the claim generation so an old plan cannot
silently regain authority.

Rollback for registry/discovery is to stop using the project commands and leave
`projects.db` untouched for a compatible future reader. Association compensates a newly
created workspace registration if project binding fails. Clone additionally removes only
its exact reserved directory on an ordinary failure; a replaced path is preserved for
manual review. Existing repositories are never rewritten, moved or deleted.

## Managed task worktrees

Durable parallel tasks never reuse the canonical checkout as a writer. The Edge creates
each worktree below the fixed private namespace
`<development-root>/.mcp-devbox-worktrees/<project>/<worktree-id>` from one exact
40-character base commit and a generated `codex/worktree-<id>` branch. The caller cannot
supply a path or branch.

The manager validates the canonical repository, `.git` worktree pointer, top-level,
branch, ownership, modes and absence of symlinks. It registers the worktree as a separate
`linux-workcell/dev` workspace. Every writer is bound to one durable job, lease and fence;
a restarted coordinator may reclaim it only for the same job with a strictly newer fence.
Status and cleanup revalidate the physical worktree and registry binding. Cleanup rejects
dirty trees, removes only the exact worktree/workspace and retains the branch and task
evidence for normal review and integration.

Git linked worktrees keep their index and per-worktree metadata below the canonical
repository's common `.git/worktrees` directory rather than below the worktree itself.
The Codex workcell therefore binds that one validated common Git directory to a fixed
sandbox target and points `GIT_DIR` at the exact generated worktree entry. The linked
entry resolves its own `commondir`; the launcher does not synthesize or override it.
The canonical checkout contents and sibling worktree contents are not mounted. Pointer,
root, ownership, mode and symlink checks fail closed before any worker starts.

The Edge scheduler admits a bounded number of independent operations. Read/status,
process and project work use shared capacity; signed bundle update, rollback and repair
take an Edge-wide exclusive gate. A long operation in project A therefore does not hold
the execution gate for status in project B. Leases, fences and terminal journal writes
remain durable, and completion validation plus the terminal write occur under one store
lock to prevent cancellation/recovery races.

Process identity is captured at start: project owner/repository, claim generation,
profile, mode, workspace binding and the OS process identity are persisted with the
journal row. A process-specific status, stdin, signal, stop or cleanup request validates
that durable binding and the PID/start-time identity; it does not re-resolve the current
project registration or require a clean Git tree. A list request, or cleanup without a
process ID, resolves the current registered alias and target without running Git so a
reassociated alias cannot reach records from its former workspace. This allows an
authorized process to be observed or stopped after ordinary source edits or a later
registry release. Legacy process rows are migrated with conservative compatibility
fields and remain subject to the old binding checks when the new identity is
unavailable.

## Toolbox lifecycle and migration

Toolbox records are versioned and bind workspace identity, mount policy, rootless engine
identity and generation. A compatible stopped toolbox may be reconciled or restarted;
an identity, ownership, symlink or mount-boundary change fails closed and requires
controlled recreation. Reconciliation never deletes or resets the source checkout.
Legacy records remain readable and are upgraded only after their recorded owner,
workspace and mounts pass validation. Toolbox cleanup is explicit and scoped to the
server-owned toolbox container and its record. Browser harness runs and artifacts
under the workspace's legacy `.mcp-devbox` data are removed only by their explicit
harness cleanup operation; toolbox cleanup does not remove them.
The per-workspace runtime and cache roots are retained for reuse; removing those
roots requires a separate administrator-controlled workspace cleanup and is never
an implicit toolbox operation.

Toolbox lifecycle operations use a shared lock keyed by the validated Edge state root
and workspace ID. This prevents concurrent managers in one Edge process from losing
record updates or recreating the same container twice, while allowing unrelated
workspaces to proceed independently. The supported deployment invariant is one
managed Edge process per state root; multiple Edge processes sharing that root are
not a supported concurrency configuration and must be rejected by service startup or
run with separate state roots. The lock is not a substitute for the durable record,
container labels, mount fingerprints or ownership checks.

## Direct GPT Web snapshot vertical

`project_snapshot` is the first direct GPT Web to Edge operation. The caller supplies
only a project alias, a human Edge target alias and a caller-generated idempotency key.
The control plane persists the complete normalized request in the existing durable Edge
operation queue. It does not create a model runtime or a model turn.

The paired Edge resolves the project and target through `projects.db`, revalidates the
owner-bound checkout and selects the registered `linux-workcell/dev` workspace. It then
runs exactly these fixed Git commands in that workspace:

```text
git rev-parse --verify HEAD
git branch --show-current
git status --porcelain=v1 --untracked-files=normal
```

The response is bounded to operation identity, reuse state, project/repository/target,
profile/mode, branch, commit and clean state. Device identity, workspace identity, paths,
command output and credentials are not returned. A dirty checkout is reported as project
state `dirty` with `clean=false`; it remains resolvable so later status, execution, process,
toolbox and Git inspection calls can continue. Registration and fast-forward keep their
explicit clean-tree preconditions and never overwrite local changes.

Repeating the same normalized request and idempotency key returns the original durable
operation, including a terminal result. This allows the same GPT conversation to retry
after an MCP transport reconnect without duplicating work. A different key creates a new
snapshot operation. OpenCode and the model-turn path remain unchanged as a fallback.

## Current limitations

The v2 implementation deliberately keeps recovery bounded:

- discovery remains limited to direct children of the approved development root;
- the public project alias surface does not expose arbitrary registry paths or internal
  project/workspace identifiers;
- Edge capacity is bounded by the installed worker pool and is not caller-selectable;
- exact-head CI and real Linux/Windows device acceptance are still required before a
  release can claim the v2 behavior.

These limits preserve the authority model. They are not reasons to classify a normal
dirty checkout, toolchain cache or long-running process as unsafe.

## Verification

Local gates for this implementation include:

```text
go test ./internal/edgeclient ./cmd/mcp-edge -count=1
go test ./... -count=1
go vet ./...
staticcheck ./...
git diff --check
```

Exact-head CI remains authoritative for race, package, Bubblewrap, rootless and other
remote environment gates.
