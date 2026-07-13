# P1 tool catalog modularization

Status: in progress on branch `p1-tool-catalog-runtime`.

Completed:
- Step 28: introduced `internal/mcpserver/catalog` and moved `system_runtime_info` into `RegisterRuntime`.
- Step 29 candidate: added shared schema helpers and a narrow `NotesService` interface, then moved `notes_list`, `notes_read`, `notes_write_preview`, and `notes_write` into `RegisterNotes` at their original registration position.

Compatibility preserved:
- 62 public tools;
- deterministic catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- same names, order, descriptions, schemas, versions, annotations, handlers, aliases, approval posture, and environment variables.

Verification for Step 29 candidate:
- catalog notes RED test failed before implementation;
- `go test ./internal/mcpserver/catalog -run TestRegisterNotes -count=1` passed;
- `go test ./... -count=1` passed;
- `go vet ./...` passed;
- `go build ./...` passed;
- `mcp-catalog-smoke` matched production count/hash.

Capabilities decision: defer a generic capability dispatcher until P1-P3 are stable. Reassess only if the public tool catalog continues to grow/change frequently; keep current typed tools as the secure compatibility surface meanwhile.

Next recommended domain: memory/handoff tools, because they are adjacent, local, and can use another narrow interface without external-service coupling.
