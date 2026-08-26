# P16 project aliases and workspace resolution

Status: **P16 Step 3 alias registry, bounded recovery, approved prepare/status tools
and owner-bound clone implemented on `p16-global-work-scheduler`. Validation is pending
for the new exact PR head and a real Parrot execution; the previous Step 3 head passed
all 15 remote gates.**

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
ready or blocked state
workspace profile and mode when ready
stable blocker reason when blocked
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

The initial schema version is `1`. SQLite is configured with one connection, full
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

Every registration and resolution revalidates the physical checkout. The default Linux
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

A checkout is ready only when:

- the registered workspace remains safe and below an approved root;
- `.git` is a real directory, not a symlink;
- the top-level checkout equals the registered workspace path;
- fetch and push remotes are owner-bound HTTPS GitHub URLs for the exact repository;
- tracked and untracked status is clean, except for untracked files below the
  Edge-owned `.mcp-devbox/` runtime namespace.

The Edge creates `.mcp-devbox/tools`, `.mcp-devbox/cache`, and
`.mcp-devbox/runtime` inside trusted development workspaces. Those untracked runtime
artifacts do not make a checkout dirty. The exception is deliberately narrow: a
tracked, staged, renamed, deleted, or otherwise changed path remains dirty even when
it is below `.mcp-devbox/`, and similarly named paths outside that exact namespace are
not ignored. This prevents the Edge's own tool caches and runtime HOME from blocking
project resolution without hiding repository changes.

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
project_registration_failed
credential_unavailable
clone_failed
cleanup_required
```

Errors do not include paths, remote URLs, Git output or internal IDs.

## Discovery and recovery decisions

Discovery validates the development root and reads at most 128 direct children by
default, with a hard maximum of 512 and one 30-second total deadline. It never recurses,
follows a symlink, creates a
directory, registers a workspace or changes Git state.

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
`ambiguous_checkout`; no path is guessed. A dirty unique match remains blocked. No match
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

Alias, repository, target, profile and binding metadata survive close/reopen. The
registry is additive: existing low-level workspace tools and `workspaces.db` retain
their current behavior.

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

This Step 3 cut does not yet:

- discover recursively or outside direct children of the approved development root;
- continue, test, build or deploy a project through the scheduler;
- calibrate or allocate per-Edge resource pools;
- prove the new prepare flow on the real Parrot Edge.

Those later operations remain blocked until their durable-job, scheduler, resource and
real-device contracts are implemented.

## Verification

Local gates for this cut include:

```text
go test ./internal/edgeclient ./cmd/mcp-edge -count=1
go test ./... -count=1
go vet ./...
staticcheck ./...
git diff --check
```

Exact-head CI remains authoritative for race, package, Bubblewrap, rootless and other
remote environment gates.
