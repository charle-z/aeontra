# Brain operations runbook

## Purpose

P9 Brain is persistent cross-repository memory anchored to the MCP Devbox server. The
source of truth is strict Markdown with YAML frontmatter. SQLite FTS5 is only a
redacted disposable cache and local Git is only a rollback history. Brain adds no
service, port, worker, queue, model, or remote database.

The deployed console is not part of this milestone and remains unchanged. Until the
P9 release PR is merged and deployed, production remains P8 with 62 tools.

P9 release-candidate head `96f7ca15183271772aecbf2d0ac2cceb88e20e5d` is
merge-ready with evidence in `docs/baselines/2026-07-14-p9.md`. Production volume/env
setup, exact deployment identity, both smoke commands and the annotated `p9` tag remain
mandatory; no console or OAuth change is authorized during those operations.

## Runtime configuration

Brain is controlled by one optional environment variable:

```text
MCP_DEVBOX_BRAIN_ROOT=/brain
```

When the variable is unset or empty:

- all 67 tools remain registered so the catalog is deterministic;
- the five `brain_*` tools fail with `brain is not configured`;
- no Brain directory, Git repository, or SQLite file is created.

When set:

- the path must be absolute;
- it must be disjoint from every general repository root;
- it must not contain symlink ancestors;
- the process creates or validates private `curated/`, `working/`, and `.cache/`
  directories;
- a local Git repository is initialized with no remote;
- `.cache/brain.db` is opened and checked;
- all Markdown truth is reindexed before the server begins accepting traffic;
- any permission, schema, source, Git, cache, duplicate-slug, remote, or reindex error
  fails startup instead of silently disabling Brain.

Recommended production layout:

```text
/repos   general repositories, mounted separately
/state   OAuth client/refresh state, mounted separately
/brain   Brain Markdown + local Git, dedicated persistent volume
```

Never set `MCP_DEVBOX_ROOT=/brain`, place `/brain` below `/repos`, or expose `/brain`
as a repository alias.

## Coolify deployment

Use the existing MCP Devbox application. Do not create a new application or service.

1. Add a persistent volume mounted at `/brain`.
2. Set:

   ```text
   MCP_DEVBOX_BRAIN_ROOT=/brain
   ```

3. Keep the existing `/repos` and `/state` volumes.
4. Deploy only after the P9 pull request gates are green.

The image creates `/brain` for UID/GID `10001:10001`. For a host bind mount rather
than a Docker-managed volume, the operator must make the mounted directory owned by
that UID/GID and mode `0700` before starting the application. Do not solve permission
failures with world-writable modes.

The first healthy startup creates:

```text
/brain/.git/
/brain/.gitignore
/brain/curated/
/brain/working/
/brain/.cache/brain.db
```

Expected permissions:

- root, `.git/`, `curated/`, `working/`, `.cache/`: `0700`;
- `.gitignore`, Markdown notes, SQLite file: `0600`.

## Owner curation

Agents cannot write `curated/`. The owner edits curated notes out-of-band through a
private operator session or a future owner-only console flow. Every curated note must
use `author: owner` and the exact frontmatter contract in `specs/006-brain/spec.md`.

After an owner edit, run a bounded rebuild through MCP:

```text
brain_index {"action":"reindex"}
```

A malformed note, duplicate slug, unsafe permission, unexpected file, or secret-like
path condition blocks the rebuild and leaves the previous index snapshot intact.
Curated out-of-band changes are not automatically committed by MCP Devbox; include
them in the operator's backup procedure.

Agents write only through `brain_write`, which targets `working/<slug>.md`, rejects
secret-shaped content, requires provenance and `review_by`, updates the cache, and
creates one local Git commit.

## Verification

After deployment, run from the expected source commit with a valid bearer credential
available only in an environment variable:

```bash
go run ./cmd/brain-smoke \
  --url https://mcp.example.com \
  --expected-commit "$(git rev-parse HEAD)"
```

The default credential variable is `MCP_DEVBOX_TOKEN`. `--bearer-env NAME` can select
another environment variable containing a valid bearer access token. The command
never prints the credential, note text, query, context digest, slug, provenance, or
private path. It prints only runtime identity, index readiness/schema, note count, and
context byte count.

Also verify:

```text
brain_index {"action":"status"}
brain_context {"limit":8}
```

`brain_context` is demand-driven and bounded. It is not automatically injected into
every MCP session.

## Backup

The authoritative backup set is:

```text
/brain/.git/
/brain/.gitignore
/brain/curated/
/brain/working/
```

`.cache/` may be excluded because it is regenerated. Do not treat a copy of
`brain.db` as a backup.

For a consistent filesystem backup:

1. Stop or quiesce the existing application.
2. Snapshot/copy the authoritative set with owner/mode metadata preserved.
3. Restart and run `brain-smoke`.

Do not configure a Git remote merely as a backup shortcut. P9 intentionally rejects
any remote to prevent accidental note exfiltration.

## Restore

1. Stop the application.
2. Restore `.git/`, `.gitignore`, `curated/`, and `working/` to the dedicated volume.
3. Restore directories to `0700` and files to `0600`, owned by UID/GID `10001:10001`.
4. Remove `.cache/brain.db` if present.
5. Confirm `.git/config` has no remote.
6. Start the application. Startup performs a complete strict reindex.
7. Run `brain-smoke` and inspect `brain_index status`.

If startup fails, keep the old healthy application serving and correct the source or
volume offline. Do not suppress the failing note or lower validation.

## Update

P9 schema version 1 requires no authoritative data migration. Application updates may
replace the disposable cache schema only after tests prove full reconstruction from
Markdown. Keep the persistent volume mounted during rolling replacement.

During the short old/new container overlap, do not issue Brain writes. SQLite and
local Git are single-process coordinated; rolling overlap is for health handoff, not
parallel writers.

## Rollback

Application rollback is independent of data rollback:

1. Deploy the known-good `p8` tag or another pre-P9 commit.
2. Unset `MCP_DEVBOX_BRAIN_ROOT` for that application version.
3. Keep the `/brain` volume intact and unmounted from the old binary if desired.
4. Do not delete or rewrite Brain data.

To roll back one agent note while running P9, use the local Brain Git history through
an operator session, restore the intended Markdown state, then perform a full reindex.
There is intentionally no MCP tool for arbitrary Git checkout or deletion inside
Brain.

## Troubleshooting

### `brain is not configured`

The 67-tool catalog is loaded, but `MCP_DEVBOX_BRAIN_ROOT` is unset. Set the variable
and persistent mount, then redeploy the existing application.

### Startup reports root overlap

The Brain path equals, contains, or is contained by a repository root. Move it to a
dedicated volume such as `/brain`; do not weaken the check.

### Startup reports private-directory or permission failure

Verify ownership `10001:10001`, directory mode `0700`, file mode `0600`, and absence
of symlinks. Do not use `0777`.

### Startup reports a Git remote

Remove the remote offline after confirming it was not used to exfiltrate notes. P9
accepts only a local repository.

### Reindex fails

Check strict frontmatter, file/slug agreement, duplicate slugs across both trust
levels, unexpected files/directories, permissions, sizes, timestamps, links, and note
count/aggregate-byte limits. The previous cache snapshot remains available when a
manual reindex fails.

### Cache corruption

Stop the application, remove only `.cache/brain.db`, and restart. Never remove the
Markdown or `.git/` source history as a cache repair.

### Connector shows only 62 tools

First verify `/version` serves the P9 commit, `tool_count=67`, and the P9 catalog hash.
If the server is correct, reconnect or refresh the client catalog. Do not redeploy
solely because the client cached the P8 list.
