# P16 project aliases and workspace resolution

Status: **P16 Step 3 first cut implemented on `p16-global-work-scheduler`; validation
pending until exact-head CI is green. Automatic discovery, association and clone remain
unfinished.**

This document is the repository source of truth for the durable project registry and
its local read-only alias interface. The Brain may index it but does not replace it.

## Purpose

A normal project lookup uses a human alias rather than copied device, workspace,
runtime, job or idempotency identifiers.

The current local interface is:

```text
mcp-edge project status --alias ekoparty [--target parrot]
mcp-edge project resolve --alias ekoparty [--target parrot]
```

These commands accept no state path, workspace ID, device ID, runtime ID, command, URL
or repository mutation. `status` reports a safe ready/blocked result. `resolve` returns
the same safe result but fails when the project cannot be safely resolved.

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
repository_mismatch
checkout_unsafe
registry_unavailable
```

Errors do not include paths, remote URLs, Git output or internal IDs.

## Canonical checkout inference

The pure inference helper maps repository `charle-z/ekoparty-trip-agent` to checkout
name `ekoparty-trip-agent` below the configured development root. It validates that the
result is one direct child and does not create or modify the filesystem.

Actual discovery, association and clone are deliberately separate later operations.
Inferring a path is not authority to create it.

## Persistence and rollback

Alias, repository, target, profile and binding metadata survive close/reopen. The
registry is additive: existing low-level workspace tools and `workspaces.db` retain
their current behavior.

Rollback for this cut is to stop using/removing the read-only project command and leave
`projects.db` untouched for a compatible future reader. It does not rewrite or delete
workspaces or repositories.

## Current limitations

This first Step 3 cut does not yet:

- discover unbound existing checkouts;
- detect multiple matching checkouts as ambiguous;
- associate a safe legacy path through preview/approval;
- clone a missing repository;
- create server-side project tools for ChatGPT;
- map target aliases to scheduler pools/devices;
- continue or build a project.

Those operations remain blocked until their test-first preview, local revalidation,
rollback and no-data-loss contracts are implemented.

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
