# ADR 0003: Markdown truth with pure-Go SQLite FTS5 cache for P9 Brain

- Status: Accepted
- Date: 2026-07-13
- Scope: P9 Brain persistence, retrieval, links, and resource posture
- Dependency candidate: `modernc.org/sqlite@v1.53.0`

## Context

P9 needs persistent cross-repository memory shared by authenticated agents connected to
the existing MCP server. The production VPS has 4 GB RAM, 2 vCPU, and 80 GB disk and
already runs the Coolify stack, MCP Devbox, a validation runner, and build workloads.
It is saturable. Any new resident database, vector daemon, embedding model, queue, or
worker would compete with the control plane and builds.

The personal Brain scale is expected to be thousands—not millions—of short Markdown
notes. Required behavior is provenance-aware keyword retrieval, strict trust levels,
links/backlinks, reproducible rebuild, and rollback. Semantic vector similarity is not
a demonstrated requirement at this scale.

The repository intentionally has very few runtime dependencies. Adding a SQLite driver
is therefore a real supply-chain, build-time, image-size, SBOM, and vulnerability cost
that must be explicit rather than hidden.

## Decision

### Files are truth

Brain source is strict Markdown with YAML frontmatter under a dedicated persistent
root:

```text
curated/<slug>.md
working/<slug>.md
```

The directory is a local Git repository with no remote created by P9. Markdown and Git
provide human inspection, diff, rollback, and backup portability. A note can be
understood without the index or MCP Devbox binary.

### SQLite is a disposable in-process cache

P9 uses SQLite FTS5 through the pure-Go driver `modernc.org/sqlite`, initially pinned
to `v1.53.0` after compatibility and FTS5 tests. The module reports Go 1.25 as its
minimum, compatible with the repository's Go 1.26.5 toolchain.

The DB lives under ignored `.cache/brain.db`. It stores normalized metadata, redacted
search text, forward links, and FTS5 rows. It is never authoritative and may be deleted
and rebuilt entirely from Markdown.

SQLite is not a resident service:

- no process or listening port is added;
- no database container or Coolify application is created;
- no separate credentials, network path, or backup format is introduced;
- connections exist only inside the MCP Devbox process;
- one bounded store mutex and a small connection pool constrain memory/concurrency.

Search uses parameterized SQL and FTS5 BM25. Client queries are converted to bounded
quoted plain-text terms; arbitrary FTS query syntax is not accepted.

### No embeddings in P9

P9 does not generate or store embeddings and does not run a vector index. Reasons:

- resident or per-query model cost is unjustified on the 4 GB / 2 vCPU VPS;
- remote embedding APIs would create cost, privacy, credential, network, provider,
  retention, and availability dependencies;
- vector stores add another derived format and migration surface;
- provenance/trust/review controls matter more than approximate similarity for a
  personal security-sensitive memory;
- FTS5 plus explicit `[[slug]]` links covers the proven requirement with simpler
  failure and rollback modes.

A future hybrid search remains possible only if measured retrieval failures justify
it. Any hybrid design must keep Markdown as truth, make vectors disposable, avoid a
resident service by default, preserve provenance/trust filters, and receive a separate
ADR and resource/security review.

## Alternatives rejected

### PostgreSQL/MySQL/database server

Rejected: adds a resident process/container, credentials, port, migrations, backup,
health, memory, CPU, and operational coupling for personal-scale data.

### Qdrant/Weaviate/Chroma/vector daemon

Rejected: resident service and memory cost, embeddings requirement, extra network and
backup surface, and weak benefit for current scale.

### Embedded key-value database without FTS5

Rejected: would require inventing ranking/tokenization or performing linear scans.
SQLite FTS5 already provides mature bounded full-text retrieval and transactions.

### Grep-only Markdown search

Rejected as the sole engine: simple and useful for recovery, but lacks BM25 ranking,
metadata filtering, transactional incremental updates, snippets, and efficient
backlinks at growing personal scale. It remains a manual fallback because files are
plain Markdown.

### SQLite through CGO

Rejected: the production build intentionally uses `CGO_ENABLED=0`; introducing CGO
would complicate cross-platform builds, static delivery, container toolchains, and the
existing Race/runtime distinction.

### Store the Brain inside each project repository

Rejected: defeats cross-repository memory, duplicates facts, couples retention to
project lifecycle, and allows untrusted project content to become the memory authority.

## Security consequences

Benefits:

- no new network listener or resident service;
- deterministic rebuild and human-readable recovery;
- SQL injection resistance through parameterized queries and quoted plain-text MATCH;
- cache contains redacted text rather than raw detected secrets;
- Git history provides local provenance and rollback;
- separate Brain jail avoids exposing the volume through general repository tools.

Costs and controls:

- `modernc.org/sqlite` adds a non-trivial pure-Go dependency graph. It must be pinned,
  included in Dependency Review, Govulncheck, CodeQL, SBOM, and Grype evidence, and
  upgraded only through a reviewed PR.
- Compiled binary/image size and build duration may increase. Baselines must record the
  delta; unexplained excessive growth blocks release.
- Cache corruption remains possible. Startup schema/FTS probes and full rebuild are
  mandatory; Markdown remains usable when cache is removed.
- SQLite does not judge truth. Trust level, provenance, type, review date, and owner
  curation remain explicit policy, not ranking signals hidden from users.

## Operational consequences

- Backup the persistent Brain directory, excluding `.cache/` if desired.
- Restore Markdown and `.git/`, then reindex.
- No SQL dump is required.
- Rollback the application independently from the Brain volume.
- Unsetting `MCP_DEVBOX_BRAIN_ROOT` disables the capability without deleting data.

## Compatibility

P9 adds five new MCP tools deliberately. It does not alter the names, schemas,
annotations, handlers, or semantics of the 62 P8 tools. The catalog count and hash are
expected to change only by the reviewed five-tool delta.

This ADR does not authorize P10 sandbox/egress implementation, Asset Broker, universal
profiles, Edge Agent, a public Brain API, or multi-user tenancy.
