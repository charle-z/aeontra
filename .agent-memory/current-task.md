# P9 Brain — Step 5 isolated capability

Status: Step 5 is complete locally on `p9-brain`. It builds on Step 4 commit
`c11af4a97f9e11cb9a4385e4ee2a56bf663c8938` and P8 closure
`2e3429c9d6342e8e091cadf65293c5c85b1b3259`. The invariant remains no resident service.

## Implemented

- `BrainCapability` is composed into every `tools.Service` over the existing shared
  audit/redaction core, but owns only an independently validated `brain.Store`.
- Disabled state is the default and all search/read/write/index/context operations
  return the same `ErrBrainNotConfigured` without exposing paths or partial state.
- `WithBrainStore` is a startup-only delegating facade method; operational behavior and
  close lifecycle remain on the owning capability per AST boundary tests.
- The Brain root is proven outside repository policy roots; no repository tool or
  command workdir gains access to it.
- Typed capability operations wrap store search, note+backlinks read, working write,
  status/reindex, and context digest.
- Audit spans record only safe operation classifications and bounds; query, body,
  provenance, private root, and canary values do not appear in JSONL.
- Returned title/provenance/body/excerpt/digest fields receive another shared redaction
  pass at the capability boundary.
- `ContextDigest` is on-demand, at most 16 notes/4 KiB, curated-first, recent working
  second, omits full bodies, and excludes expired working notes.
- Capability close waits for active operations, detaches and closes the store, and is
  idempotent. `appRuntime.Close` closes Brain before audit and observability logs.
- Coverage: Brain 81.7%, tools 73.9%, app 68.0%; package gates pass.

## Not implemented yet

- no five public Brain tool registrations or catalog delta;
- no `MCP_DEVBOX_BRAIN_ROOT` runtime env or persistent mount;
- no operations/runbook/smoke/deployment;
- production remains P8 with 62 tools and unchanged console.

## Console decision

The deployed console remains unchanged during P9. Do not modify UI/auth in this branch.
The owner will provide the creative BIOS-inspired visual brief; OAuth-only migration
and live task/device status belong to a separate post-P9 branch.

## Next exact actions

1. Clean helpers, run final Step 5 gates, commit/publish `Step 5`.
2. Begin Step 6 with RED catalog tests proving the original 62 definitions are
   unchanged and exactly five Brain tools are appended with bounded schemas and honest
   annotations.
3. Keep runtime env/mount configuration deferred to Step 7.
