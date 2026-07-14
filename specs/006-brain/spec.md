# Spec — P9 Brain

Status: **complete / merge-ready** on branch `p9-brain`; production verification and
the annotated `p9` tag remain post-merge release gates.

## Goal

Provide server-anchored, cross-repository memory that any authenticated MCP client can
query and contribute to without making an LLM, vector service, queue, or database
server resident on the resource-constrained VPS.

The intelligence remains in the calling model. Brain is only:

1. structured Markdown source files;
2. provenance and trust-level enforcement;
3. bounded full-text retrieval and backlinks;
4. a disposable local SQLite FTS5 cache;
5. local Git history for rollback and backup.

## Resource boundary

The production VPS has 4 GB RAM, 2 vCPU, and 80 GB disk and already runs Coolify,
MCP Devbox, the validation runner, and build workloads. P9 must add no resident
service, database server, embeddings model, vector daemon, queue, or worker process.
This is an explicit no resident service invariant, not a deployment preference.
SQLite is opened in-process only when the daemon starts or a Brain tool is called.

## Configuration

`MCP_DEVBOX_BRAIN_ROOT` is optional. When unset, all Brain tools remain registered so
the public catalog is deterministic, but every call fails with the same safe
"brain is not configured" error.

When set, the value must be an absolute path. Brain builds a dedicated jail containing
only that root; it is not appended to general repository roots and is not exposed to
repository selection, filesystem tools, command workdirs, or validation profiles.
The operator mounts this path as its own persistent volume.

Startup behavior when configured:

- create/verify the root with private permissions;
- create `curated/`, `working/`, and `.cache/`;
- initialize a local Git repository when `.git/` is absent;
- create no Git remote;
- ensure `.gitignore` ignores `/.cache/`;
- open `<root>/.cache/brain.db`;
- rebuild the disposable index from Markdown source files;
- fail startup if the configured store cannot be secured, parsed, indexed, or opened.

## Source-of-truth layout

```text
<brain-root>/
  .git/
  .gitignore
  curated/
    <slug>.md
  working/
    <slug>.md
  .cache/
    brain.db
```

Only Markdown files under `curated/` and `working/` are truth. The SQLite database is
a disposable cache and may be deleted and rebuilt at any time.

## Frontmatter contract

Every note has strict YAML frontmatter followed by Markdown:

```yaml
---
slug: deployment-rollback
title: Deployment rollback rule
type: fact
author: agent:chatgpt
created: 2026-07-13T22:00:00Z
updated: 2026-07-13T22:00:00Z
provenance: PR #3 and production smoke for commit 2e3429c
review_by: 2026-08-13
---

Body with optional [[related-slug]] links.
```

The actual keys are exactly:

- `slug`: validated kebab-case, 1–64 bytes;
- `title`: non-empty, at most 160 bytes;
- `type`: `fact|note|feedback|reference|hypothesis`;
- `author`: `owner` or `agent:<validated-name>`;
- `created` and `updated`: UTC RFC3339 timestamps;
- `provenance`: non-empty, at most 1024 bytes;
- `review_by`: optional ISO date `YYYY-MM-DD` for owner notes; mandatory for agent
  notes, not in the past, and no more than 365 days after the write.

Unknown frontmatter keys fail validation. Filename, frontmatter slug, and link slugs
must match the same kebab-case grammar. Frontmatter is server-generated for
`brain_write`; clients cannot choose timestamps.

## Trust levels

### `curated/`

- source of reviewed owner facts and references;
- every note must use `author: owner`;
- MCP tools may read/search/index it but can never create, modify, rename, or delete it;
- owner changes are made out-of-band and imported by reindexing.

### `working/`

- agent-authored hypotheses, feedback, references, and notes;
- `brain_write` always targets this directory; no collection/path parameter exists;
- tool writes require `author: agent:<name>`, provenance, and `review_by`;
- an update may modify only a note with the same existing author;
- writes are atomic, serialized, incrementally indexed, audited, and committed to the
  local Brain Git repository with fixed controlled argv and no remote operation.

Agent writes to `curated/` are structurally impossible through the public schema and
must also fail in direct package-level adversarial tests.

## Search and graph

- `[[slug]]` links are parsed from Markdown bodies.
- The index stores forward links and computes backlinks at read time.
- Search uses SQLite FTS5 with BM25 ranking over title and body.
- User queries are treated as bounded plain text rather than arbitrary FTS syntax.
- Broken links are counted in index status; they do not create files.
- Search/read/context outputs are content-redacted before return.

## Hard bounds

- one source file: at most 40 KiB;
- Markdown body: at most 32 KiB;
- indexed notes: at most 10,000;
- aggregate indexed source bytes: at most 64 MiB;
- query: 1–256 bytes;
- `top_k`: default 5, maximum 20;
- excerpt: maximum 480 bytes per result;
- search response: maximum 16 KiB;
- backlinks returned: maximum 128;
- context digest: maximum 16 notes and 4 KiB;
- one-line context entry: slug, trust level, type, title, author, review date, and
  short provenance only—never a complete note body.

## Secret and drift controls

- Agent writes are rejected when existing secret scanning detects secret-like material;
  no redacted placeholder is silently stored as truth.
- Manual source notes are redacted before entering SQLite and before being returned.
- Paths, slugs, links, and cache files are symlink-safe and jailed.
- Brain is retrieved only by explicit tool calls; no complete Brain dump is injected
  into `initialize` or every session.
- `brain_context` is bounded and on-demand.
- Agents must not duplicate code, repository state, diffs, generated documentation,
  CI logs, or other facts already recorded by the relevant source repository.
- Agent-authored conclusions remain `working/` until the owner reviews and promotes
  them out-of-band.

## Public tools

P9 adds exactly five declarative tools. The previous 62 tools retain their names,
schemas, annotations, handlers, and semantics.

### `brain_search`

Input: `query`, optional `top_k`.

Returns BM25-ranked bounded matches with slug, trust level, title, type, author,
updated/review dates, short provenance, and a short excerpt.

### `brain_read`

Input: `slug`.

Returns one redacted validated note plus bounded backlinks. Slugs are globally unique
across both trust directories; duplicates fail indexing.

### `brain_write`

Input: `slug`, `title`, `type`, `author`, `provenance`, `review_by`, and `body`.

Creates or updates only `working/<slug>.md`. It rejects `author: owner`, secret-like
content, invalid review dates, cross-author updates, traversal, symlinks, and bounds
violations. It updates the index and creates a local Git commit. It has no path,
collection, remote, command, or approval parameter.

### `brain_index`

Input: `action` (`status` or `reindex`).

`status` is read-only. `reindex` performs one serialized bounded rebuild from Markdown
truth into the disposable cache. It never changes source notes or Git history.

### `brain_context`

Input: optional `limit` up to 16.

Returns a 4 KiB startup digest, one line per selected note, preferring curated notes
and then recently updated non-expired working notes. It never returns full bodies.

## Git behavior

- Brain initializes only a local repository and never creates or contacts a remote.
- Agent writes use fixed Git argv and controlled author/committer metadata.
- One successful source write produces one local commit.
- A Git failure causes the source file and index to roll back to their prior state or
  the write fails closed; a successful tool result never leaves an uncommitted partial
  update.
- Owner-curated out-of-band changes are not automatically committed by P9.

## Concurrency

- Writes and full reindexes are serialized.
- Searches and reads may run concurrently against a consistent SQLite snapshot.
- Incremental write transactions update metadata, FTS rows, and links atomically.
- Tests run concurrent search/read/write/reindex operations under the race detector.

## Acceptance

- Strict parser validates frontmatter, trust directories, dates, slugs, links, bounds,
  global slug uniqueness, and symlink safety.
- An agent attempt to write `curated/` fails in adversarial tests.
- Secret canaries are rejected before file/index/Git mutation.
- Full reindex and incremental updates return the same search/read/backlink results.
- FTS5 BM25 search works through `modernc.org/sqlite` with CGO disabled.
- Index deletion followed by reindex restores equivalent results.
- Concurrent tests and GitHub Race/Staticcheck pass.
- Exactly five new tools are registered; the original 62 contracts are unchanged.
- Production uses a persistent Brain volume, exact release commit, healthy runtime,
  and bounded smoke tests without printing private note bodies.

## Non-goals

- No embeddings, vector store, graph database, remote database, queue, worker, model,
  recommender, autonomous summarizer, or automatic full-context injection.
- No multi-user tenancy or per-user identity inference from a shared bearer token.
- No MCP mutation of curated notes.
- No remote Git repository or automatic external backup in P9.
- No P10 sandbox/egress implementation in this milestone.
