# Plan — P5 deeper testing

Status: **active**.

## Sequence

1. **P5 foundation** — record P4 as deployed, create this spec/plan/tasks, freeze the
   62-tool compatibility expectation, and confirm the ordinary suite.
2. **Race and concurrency** — run `go test -race ./...`; add deterministic stress
   tests around shared maps/stores and fix only proven races.
3. **Fuzz/adversarial targets** — add Go fuzz functions with curated seed corpora for
   policy paths/commands/redaction, JSON-RPC HTTP parsing, grants, and action plans.
4. **Coverage gate** — add a small tested Go command that parses a coverprofile and
   enforces versioned package thresholds for security-critical packages.
5. **Integration contract matrix** — consolidate synthetic end-to-end tests for
   transports, auth, catalog, grants, plans, and runtime identity.
6. **Closure** — update docs/baseline, audit commits/files, run all P5 gates, verify the
   deployed P4 catalog, and issue the merge-readiness verdict.

## Test design rules

- Tests are deterministic by default; time, randomness, external clients, and runners
  are injectable.
- Concurrency tests use bounded goroutines and explicit synchronization rather than
  arbitrary sleeps.
- Fuzz targets assert invariants and must not execute arbitrary commands or contact the
  network.
- Coverage measures behavior, not line-count vanity; exemptions require a documented
  reason and owner.
- Integration tests use loopback/synthetic services and temporary directories.
- Failures must identify the package, invariant, and smallest reproducible input.

## Verification per step

```text
go fmt ./...
go test ./... -count=1
go vet ./...
go build ./...
```

Additional P5 gates are introduced only after their local implementation and tests are
green. P6 later makes selected gates mandatory in GitHub Actions.
