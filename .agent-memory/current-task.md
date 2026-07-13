# P1 tool catalog modularization

Status: closing P1 on branch `p1-tool-catalog-runtime`.

Completed through Step 50. Current Step 51 candidate:
- added `internal/mcpserver/catalog/annotations.go` with declarative MCP behavior-hint groups;
- moved all annotation assignments behind `RegisterAnnotations` while preserving exact hints, tool membership, alias coverage, and application order;
- added a focused exact-posture contract test.

Compatibility preserved:
- 62 public tools;
- catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- truthful read/write/destructive/open-world annotations unchanged.

Step 51 verification:
- RED failed because `RegisterAnnotations` did not exist;
- focused and full tests passed;
- `go vet ./...`, `go build ./...`, diff review, and production catalog smoke passed.

Next closure steps: enforce the catalog boundary and remove dead monolithic helpers, then complete the P1 closure audit. No publish, merge, or deploy yet.
