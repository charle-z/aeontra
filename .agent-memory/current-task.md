# P1 tool catalog modularization

Status: closing P1 on branch `p1-tool-catalog-runtime`.

Completed through Step 49. Current Step 50 candidate:
- added `internal/mcpserver/catalog/aliases.go` with declarative `Alias` entries;
- moved all 12 compatibility aliases behind `RegisterAliases` while preserving exact names, targets, descriptions, handlers, schemas, policy paths, and order;
- added a focused exact-order contract test.

Compatibility preserved:
- 62 public tools;
- catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- public contracts and annotations unchanged.

Step 50 verification:
- RED failed because `Alias` and `RegisterAliases` did not exist;
- focused and full tests passed;
- `go vet ./...`, `go build ./...`, diff review, and production catalog smoke passed.

Next closure steps: declarative annotations, catalog-boundary cleanup, and P1 closure audit. No publish, merge, or deploy yet.
