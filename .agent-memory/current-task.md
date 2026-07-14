# P9 Brain — Step 4 disposable FTS5 index

Status: Step 4 is complete locally on `p9-brain`. It builds on Step 3 commit
`d6654b13214c6c7c170d64a2b905efdd122f1b62` and P8 closure
`2e3429c9d6342e8e091cadf65293c5c85b1b3259`. The invariant remains no resident service.

## Implemented

- Exact direct dependency `modernc.org/sqlite@v1.53.0`, compatible with the Go 1.26.5
  project and executing with `CGO_ENABLED=0`.
- Private disposable `.cache/brain.db` with mode 0600, symlink/broad-permission denial,
  FTS5/schema/integrity probes, exact schema version, per-connection security/busy
  pragmas, bounded connection pool, and safe close/reopen.
- Transactional full rebuild from strict Markdown truth with global slug uniqueness,
  note-count/aggregate-byte limits, malformed-source fail-closed behavior, and previous
  snapshot preservation.
- Incremental metadata/FTS/link updates coordinated with atomic working-note writes and
  rolled back if the subsequent local Git commit fails.
- BM25 search over title/body. Input is parsed into bounded terms and emitted as quoted
  FTS literals, so client FTS operators/wildcards are not executed as syntax.
- Bounded results, provenance, UTF-8 excerpts, top-k, response bytes, backlinks, query
  bytes/terms, broken-link counts, and safe index status.
- Manual source secrets are redacted before cache insertion and again before return;
  a canary is proven absent from the SQLite file.
- Cache deletion/reindex equivalence and concurrent search/write/reindex tests pass.
- `internal/brain` coverage is 81.5% against an 80% gate. Both fuzz targets pass.

## Gates

- `go test ./... -count=1`: pass.
- atomic full coverage + `coverage-gate`: pass.
- `go vet ./...`, `go build ./...`, actionlint 1.7.12: pass.
- Govulncheck 1.6.0: no vulnerabilities.
- Local Staticcheck: blocked before analysis by unwritable production cache path.
- Local Race: blocked because `CGO_ENABLED=0`; both remain runner-authoritative.

## Not implemented yet

- no Brain capability composition or disabled-safe configuration;
- no five MCP tools or catalog change;
- no `MCP_DEVBOX_BRAIN_ROOT` runtime env, persistent mount, runbook, smoke, or deploy;
- production remains P8 with 62 tools.

## Console decision

The current deployed console is intentionally preserved unchanged during P9. Do not
modify its UI or authentication in this branch. The owner will provide the visual
brief for a creative BIOS-inspired operations console; OAuth-only migration and live
task visibility belong to a separate branch after P9 closure.

## Next exact actions

1. Clean Step 4 helpers, run final diff checks, commit/publish `Step 4`.
2. Begin Step 5 with RED tests for an isolated disabled-safe Brain capability sharing
   audit/redaction but never repository roots, plus safe close/concurrency behavior.
3. Keep the existing 62-tool catalog unchanged until Step 6.
