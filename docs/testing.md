# Testing and verification

This document records reproducible local commands, environment prerequisites, and
honest gate status. A gate is never described as green when it did not execute.

## Fast local suite

```text
go fmt ./...
go test ./... -count=1
go vet ./...
go build ./...
```

These commands are the per-step baseline. They do not replace race, fuzz, coverage,
or integration gates.

## Race detector baseline — P5 Step 79

Canonical command:

```text
CGO_ENABLED=1 go test -race ./... -count=1
```

Observed environment on the deployed builder container:

```text
GOOS=linux
GOARCH=amd64
CC=gcc
CGO_ENABLED=0
```

Running `go test -race ./... -count=1` returned:

```text
go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
```

Result: **blocked before tests executed**. This must not be reported as green and is
not evidence that the repository is race-free.

The production container deliberately uses `CGO_ENABLED=0` for normal builds. P5 does
not mutate persistent `go env` state or weaken runtime configuration merely to run a
development gate. P6 must execute the canonical command in GitHub Actions or another
approved ephemeral Go 1.26 environment with CGO and a C compiler enabled, retain the
result, and make the gate blocking only after it is stable.

## P5 deeper-testing sequence

1. Add bounded deterministic concurrency tests around shared state.
2. Add fuzz targets with curated safe seed corpora; regular `go test` runs the seeds.
3. Generate a coverage profile and apply package-specific thresholds.
4. Run hermetic integration contracts for transport/auth/catalog/grants/plans.
5. Re-run the race command in the P6 environment.

## Safety rules

- Do not run active DAST against production.
- Do not persist global Go environment changes on the production container.
- Do not add credentials, real tokens, private targets, or source snapshots to test
  corpora or CI artifacts.
- Tests that need time, randomness, HTTP peers, runners, or stores use injected clocks,
  deterministic ids, loopback synthetic servers, and temporary directories.
- A skipped or prerequisite-blocked gate remains visible as blocked; it is never
  silently converted to a pass.
