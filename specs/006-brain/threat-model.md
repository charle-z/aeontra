# Threat model — P9 Brain

Status: **active**.

## Assets

- Curated owner knowledge and working agent notes.
- Provenance, review dates, links, backlinks, and local Git history.
- Brain root path and local SQLite cache.
- Existing MCP credentials, repository contents, secrets, and private infrastructure
  data that must never be copied into Brain.

## Trust boundaries

1. Authenticated MCP clients are agents, not owners, for Brain writes.
2. `curated/` is owner-controlled out-of-band input and agent-read-only.
3. `working/` accepts only the closed `brain_write` schema.
4. Markdown files are truth; SQLite and derived links/backlinks are untrusted,
   disposable cache state.
5. The Brain root has its own jail and is not a general repository root.
6. Local Git is a rollback mechanism, not a network integration; no remote is created
   or contacted.
7. Manual owner files may be malformed or contain secret-like content and must fail
   closed or be redacted before indexing/return.

## Threats and controls

| Threat | Control |
|---|---|
| Agent writes to `curated/` | No collection/path input; tool target is hard-coded to `working/`; direct package-level policy check rejects curated; adversarial test. |
| Path traversal through slug/link | Strict kebab-case grammar; filename/frontmatter/link agreement; dedicated jail; symlink rejection; fuzz target. |
| Agent claims owner authority | `brain_write` accepts only `agent:<validated-name>` and rejects `owner`; curated parser requires owner only for out-of-band files. |
| Agent overwrites another agent's note | Existing working note author is immutable; cross-author update fails. |
| Secret enters persistent memory | Existing secret scanner runs over all generated frontmatter and body; any detected agent write is rejected before mutation; canary tests. |
| Secret in manual owner note reaches search | Full/incremental indexing redacts content before SQLite; reads redact again; index never stores raw secret matches. |
| SQLite becomes source of truth | DB lives under ignored `.cache/`; all records rebuild from Markdown; deletion/reindex equivalence test. |
| SQL/FTS injection | Parameterized SQL; query converted to bounded quoted plain-text terms; no raw MATCH syntax accepted. |
| Memory feedback loop turns hypotheses into facts | Agent writes only working notes with provenance and review date; curated owner-only; type remains explicit; no automatic promotion. |
| Full Brain leaks into every session | No initialize injection; explicit bounded `brain_context`; search/read demand-driven. |
| Brain duplicates source repositories | Tool descriptions/spec prohibit code, diffs, generated docs, CI logs, and repo state; provenance points to the authoritative source instead. |
| Oversized memory/DoS | Per-file/body/query/result/context/note-count/aggregate-byte limits; one serialized reindex; bounded top-k/backlinks. |
| Reindex races with writes/searches | Store mutex and SQLite transactions; consistent snapshots; race/concurrency tests. |
| Symlink swaps cache/source/Git paths | Lstat/EvalSymlinks on root, trust directories, source targets, cache, and longest existing prefix; atomic temp/rename in same private directory. |
| Partial source/index/Git update | Serialized write transaction with prior-state backup; source/index rollback if controlled local Git commit fails. |
| Malicious local Git config/hooks execute code | Git commands disable hooks and optional config, use fixed argv/environment, no shell, no remote operation; repository ownership is validated. |
| Git remote exfiltrates notes | P9 never creates/fetches/pushes a remote; controlled write path invokes only local init/add/commit/status operations. |
| Brain root becomes a general workspace | It is not appended to `Config.Roots`; only the Brain capability receives its dedicated jail/store. |
| New resident service consumes the VPS | P9 adds no new resident service, database server, model, queue, worker, port, or Coolify application; SQLite is in-process and on-demand. |
| Cache corruption or unsupported FTS5 | Startup/reindex performs schema/version/FTS probe; cache can be deleted/rebuilt; configured Brain failure blocks startup. |
| Unknown YAML fields hide data | Strict YAML decoder with known fields only; duplicate keys and malformed frontmatter fail. |
| Stale agent notes persist forever | Agent `review_by` is mandatory and bounded; expired working notes are omitted from context and marked in search/read metadata. |
| Client passes fake timestamps | `brain_write` generates created/updated server-side; update preserves original created timestamp. |
| Tool errors leak paths/content | Generic public errors, redacted outputs, audit summaries use slug/action only; no Brain root or note body in observability. |

## Stop conditions

P9 must not merge if any test or review shows:

- an MCP path to create/modify/delete `curated/`;
- raw secret-shaped content persisted or indexed by `brain_write`;
- traversal, absolute path, separator, dot-segment, Unicode-confusable, or symlink escape;
- arbitrary FTS/SQL syntax reaching SQLite;
- unknown/unbounded fields in note/search/context output;
- unbounded note count, bytes, top-k, backlinks, or context size;
- index inconsistency after concurrent write/reindex/search;
- a local Git command capable of running hooks, a shell, or a remote operation;
- Brain root exposure through general repository tools;
- automatic full-Brain injection;
- a new resident service, embeddings model, queue, vector daemon, or database server;
- any change to the original 62 tool contracts.

## Residual risks

- Agents can write plausible but incorrect working notes. Provenance, explicit type,
  review dates, owner-only curation, and demand-driven retrieval reduce but do not
  eliminate this semantic risk.
- Owner-authored curated files may contain incorrect facts; Brain preserves source and
  provenance rather than independently judging truth.
- A process compromise that can read the Brain volume can read non-secret memory.
  P9 does not provide encryption at rest.
- The shared MCP bearer/OAuth context does not cryptographically attest the requested
  `agent:<name>` author. P9 treats it as declared provenance, never owner authority.
