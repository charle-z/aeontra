# P16 project aliases and workspace resolution

Status: **P16 Step 3 registry plus recovery cut implemented on
`p16-global-work-scheduler`; validation pending until exact-head CI is green. Bounded
discovery and internal preview/apply association are implemented. Clone and public
approval-tool integration remain unfinished.**

This document is the repository source of truth for the durable project registry and
its local read-only alias interface. The Brain may index it but does not replace it.

## Purpose

A normal project lookup uses a human alias rather than copied device, workspace,
runtime, job or idempotency identifiers.

The current local interface is:

```text
mcp-edge project discover --alias ekoparty --repository ekoparty-trip-agent
mcp-edge project status --alias ekoparty [--target parrot]
mcp-edge project resolve --alias ekoparty [--target parrot]
```

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

The Step 3 core exposes an internal typed registration operation for future approved
preview/execute tools. There is no public or local free-form `project register` command
in this cut.

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
git status --porcelain=v1 --untracked-files=all
```

Host and global Git configuration are disabled, hooks are disabled, file-protocol
access is denied, terminal prompting is disabled, execution is time-bounded and output
is bounded.

A checkout is ready only when:

- the registered workspace remains safe and below an approved root;
- `.git` is a real directory, not a symlink;
- the top-level checkout equals the registered workspace path;
- fetch and push remotes are owner-bound HTTPS GitHub URLs for the exact repository;
- tracked and untracked status is clean.

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
yields `clone_required`, but this cut does not clone it.

The safe discovery JSON contains alias, owner/repository, recovery state, candidate count
and a stable reason. It omits all candidate paths and internal identifiers.

## Association preview/apply transaction

The core exposes an internal typed preview/apply operation for a clean canonical or unique
legacy checkout. It can associate one unique safe legacy path without moving it. The safe
preview reports `approval_required`, alias, owner/repository,
target, profile and action; the reviewed plan keeps the candidate path internal and expires
after five minutes.

Apply re-runs discovery and requires the same action and exact candidate. A new match, dirty
checkout, remote change, symlink replacement, root escape or expired plan returns
`plan_changed` or `plan_expired` before mutation.

After revalidation, apply registers the existing path in `workspaces.db` and binds the
project. It never renames, copies or deletes the checkout. If project binding fails after a
new workspace registration, that registration is removed; the checkout remains untouched.
An already registered compatible workspace is reused. One workspace cannot be bound
silently to two projects.

## Canonical checkout inference

The pure inference helper maps repository `charle-z/ekoparty-trip-agent` to checkout
name `ekoparty-trip-agent` below the configured development root. It validates that the
result is one direct child and does not create or modify the filesystem.

Inferring a path is not authority to create it. Discovery and association use separate
read-only and consequential phases; clone remains a later closed operation.

## Persistence and rollback

Alias, repository, target, profile and binding metadata survive close/reopen. The
registry is additive: existing low-level workspace tools and `workspaces.db` retain
their current behavior.

Rollback for registry/discovery is to stop using the project commands and leave
`projects.db` untouched for a compatible future reader. Association compensates a newly
created workspace registration if project binding fails. It never rewrites, moves or
deletes a repository.

## Current limitations

This Step 3 recovery cut does not yet:

- expose association apply as a public/server approval tool;
- clone a missing repository;
- discover recursively or outside direct children of the approved development root;
- create server-side project tools for ChatGPT;
- map target aliases to scheduler pools/devices;
- continue or build a project.

Those operations remain blocked until their test-first authority, credential, rollback and
no-data-loss contracts are implemented.

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
