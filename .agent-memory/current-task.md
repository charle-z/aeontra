# P1 tool catalog modularization

Status: closing P1 on branch `p1-tool-catalog-runtime`.

Completed through Step 51. Current Step 52 candidate:
- added `internal/mcpserver/catalog_boundary_test.go`, which AST-checks `tools.go` and rejects direct `Server.add` registration or legacy schema helpers outside the catalog;
- removed dead `object`, `strProp`, `strArrProp`, `boolProp`, `intProp`, and `Server.add` helpers from `internal/mcpserver/tools.go`;
- updated the handler-error server test to register its synthetic tool through `addCatalogTool`, matching the new boundary.

Step 52 verification:
- RED reported every legacy helper and direct registration path;
- focused boundary test passed after cleanup;
- `go test ./... -count=1`, `go vet ./...`, `go build ./...`, diff review, and production catalog smoke passed;
- production remains at 62 tools with catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

Next: Step 53 P1 closure audit, documentation sync, branch-vs-main review, and merge-readiness verdict. No publish, merge, or deploy yet.
