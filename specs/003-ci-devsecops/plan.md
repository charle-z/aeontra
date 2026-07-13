# Plan — P6 CI/DevSecOps

Status: **complete**.

## Sequence

1. **P6 foundation** — record P5 deployment, define P6, freeze runtime/catalog scope.
2. **Workflow policy guard** — parse repository workflows and reject unsafe triggers,
   permissions, secrets, production DAST, and missing timeouts.
3. **Core CI** — formatting, atomic coverage, package gate, vet, build, staticcheck,
   govulncheck, and CGO race jobs.
4. **Security workflows** — CodeQL, dependency review, Docker build, SBOM, and local
   vulnerability scan with minimal permissions.
5. **Scheduled fuzz** — run each known fuzz target with a fixed budget and no secrets.
6. **Observed execution and remediation** — publish through a pull request, inspect
   every job/check, record package/layer/reachability evidence, fix reproducible
   failures without ignores or threshold reduction, and prove zero High/Critical image
   findings before merge.
7. **Closure** — complete: baseline, audit, release posture, fast-forward merge,
   production deployment/smoke, and observed post-merge Actions.

## Design rules

- Pull-request jobs use read-only contents permission and no repository secrets.
- `pull_request_target` is forbidden.
- Every job has a timeout and every tool/version is explicit.
- CI may download public tools/modules but may not deploy or contact private targets.
- Reports are bounded and contain no prompts, source snapshots, tokens, or private
  infrastructure identifiers.
- Experimental expensive jobs run on schedule/manual events until proven stable.
- Runtime Docker images are built and scanned locally; P6 does not publish images.

## Per-step verification

```text
go fmt ./...
go test ./... -count=1
go vet ./...
go build ./...
```

P5 package coverage and integration commands remain mandatory. GitHub-only gates must
be observed after branch publication before P6 can close.
