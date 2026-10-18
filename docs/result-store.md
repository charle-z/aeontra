# Bounded result store

Large MCP tool output is useful for diagnosis but should not saturate a connector or
remain in chat history by default. When a tool result exceeds 32 KiB, mcp-devbox
redacts it and persists it in the existing state volume, then returns only:

`status`, `summary`, `stages`, `exit_status`, `output_bytes`, `result_ref`, and
`expires_at`.

## Storage and bounds

- Database: `/state/results/results.db` in Docker, or `<state>/results/results.db`.
- Opaque references: `rs_` plus 32 lowercase hexadecimal characters; never paths.
- Success TTL: 24 hours. Failure TTL: 7 days.
- Logical content quota: 256 MiB. The oldest result is evicted first when needed.
- SQLite page target: 256 MiB; startup and opportunistic TTL cleanup are mandatory.
- Returned fragments: at most 16 KiB. Search: exact substring, at most 20 metadata
  matches. There are no embeddings or semantic indexes.
- Root/database permissions are `0700`/`0600`; symlink roots and database files fail
  closed.

The original unredacted output is never persisted. Search terms, opaque references,
result bodies, and fragments are omitted from audit arguments. Result tools are local,
read-only, idempotent, and cannot name arbitrary filesystem locations.

## Tools

- `result_read`: bounded continuation using `offset` and `max_bytes`.
- `result_find`: exact bounded substring search returning compact metadata only.
- `result_stage`: bounded read of one zero-based stage from returned metadata.

Small results remain wire-compatible. If a result store is not configured in an
embedded/test service, large output remains compatible. In the production runtime the
store is mandatory; startup fails closed when it cannot be secured or opened. A
persistence failure never returns the oversized body.

## Migration and rollback

No repository migration is needed. The table is created idempotently at startup. To
roll back the binary, stop the service and keep `results.db`; older builds ignore it.
Deletion is optional only while the service is stopped and loses diagnostic results,
not repository data. Never copy the database into a repository or expose it through a
volume separate from the protected state root.
