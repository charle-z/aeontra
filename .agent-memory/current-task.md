# P1 tool catalog modularization

Status: in progress on branch `p1-tool-catalog-runtime`.

Completed first behavior-preserving cut:
- introduced `internal/mcpserver/catalog` as a declarative registration package;
- moved only `system_runtime_info` into `catalog.RegisterRuntime`;
- mcpserver.Server still owns ordering, annotations, dispatch, and policy-backed handlers;
- no environment variable, public tool name, schema, description, annotation, approval posture, or behavior changed.

Verification:
- `go test ./internal/mcpserver/catalog -count=1` passed;
- `go test ./... -count=1` passed;
- `go vet ./...` passed;
- `go build ./...` passed;
- local refactor matches production: 62 tools and catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

Decision on dynamic capabilities: defer implementation until P1-P3 are stable. It may become valuable if the tool surface keeps growing or changes frequently, but a generic dispatcher now would concentrate authority and add schema/audit/policy complexity.

Next: commit this runtime registrar step, then migrate one additional low-coupling read-only domain while preserving the catalog hash.
