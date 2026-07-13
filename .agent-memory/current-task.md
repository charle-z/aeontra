# P4 targeted Layer-1 hardening

Status: in progress on branch `p4-l1-hardening` from deployed `main` commit `dd055e251c455086ddcb02bc302d9f406b05d6ce`.

Completed:
- Step 70 `8a3c118`: rejected path-qualified, drive-qualified and whitespace-disguised executable names.
- Step 71 `821c252`: resolved commands to canonical absolute paths and rejected relative/workspace-controlled PATH targets.
- Step 72 `9af06c4`: enforced grant TTL bounds in the central policy core.
- Step 73 `fe2e903`: expired, capped, pruned and deduplicated pending sensitive-read requests.
- Step 74 `78a6fff`: synchronized and tested project documentation state.
- Step 75 `fb6b796`: redacted secret-bearing audit file paths.

Current Step 76 candidate:
- HTTP JSON-RPC batches are parsed incrementally instead of unmarshalling the whole array into memory;
- empty batches now return bounded `-32600 invalid request` rather than HTTP 202 with no response;
- batches stop at item 129 and return one small `batch too large` error;
- the configured maximum of 128 valid messages remains accepted;
- updated security documentation, context capsule, roadmap evidence, documentation-state assertion and latest handoff.

Step 76 verification:
- RED failed because no batch item limit existed and the empty batch was accepted as no-op;
- focused HTTP and documentation tests passed for empty, over-limit and exactly-at-limit batches;
- `go fmt ./internal/mcpserver ./docs`, `go test ./... -count=1`, `go vet ./...`, `go build ./...`, diff review and `git diff --check` passed;
- production smoke remains healthy at P3 commit `dd055e251c455086ddcb02bc302d9f406b05d6ce`, 62 tools, and catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

Next after Step 76: either close P4 if no material Layer-1 gap remains, or continue only with a confirmed gap and RED test. Do not publish, merge or deploy P4 until phase closure.
