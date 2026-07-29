# Current task — MCP redeploy continuity

## Status

- Authoritative plan: Brain note `mcp-devbox-redeploy-continuity`.
- Branch: `fix/mcp-redeploy-continuity`.
- Initial validated base: `main` / `origin/main` at `b3d2c160f3179da7966254406587520b735c61ea`.
- Pull request: `#72`, `https://github.com/charle-z/mcp-devbox/pull/72`.
- Published implementation head before this continuity-only commit: `ec33804a7d66da9bcd1f431b61a0ca6e183bb6f8`.
- Hitos 0–5 complete; Hito 6 implementation and local release validation complete.
- This continuity update is the final planned repository change. After committing and publishing it, all checks for the new exact PR head must be green before merge.

## PR and checks

PR #72 targets `main` from `fix/mcp-redeploy-continuity` and is mergeable.

Initial exact-head check observation for `ec33804a7d66da9bcd1f431b61a0ca6e183bb6f8`:

- 14 checks discovered.
- 0 failed.
- 14 queued or in progress at first observation.
- Checks include Verify, Staticcheck, Race detector, Govulncheck, CodeQL, Dependency review, container SBOM/vulnerabilities, responsive Brain graph, Edge/migration, signed Debian package, rootless BuildKit, rootless Podman/PostgreSQL/Chromium, trusted host/HTB fixture and distributed OpenCode E2E.

Merge rule:

- Do not merge until the exact final PR head has at least one check, no pending checks and no failed checks.
- Use merge commit only.
- Revalidate the PR head immediately before merge.

Expected merge:

- Base: `main`.
- Feature: `fix/mcp-redeploy-continuity`.
- Expected contents: Hitos 0–6, E2E and CI integration, plus this continuity-only update.
- Production deployment must use the resulting merge commit, not the feature head.

## Final local results

Green before publication:

- `go test ./... -count=1`.
- `go vet ./...`.
- `go build ./...`.
- Staticcheck v0.7.0 over `./...` with private temporary cache.
- Actionlint v1.7.12.
- `git diff --check`.
- MCP transport/session/OAuth/console/catalog/Brain/observability/redeploy E2E suites.
- Application, integration and packaging suites.
- Catalog computed twice: `sha256:1d3646af205ec2b1a01a47d034641ac4cb8a4843d9c7879b122432308e961007` both times.
- Tool count: 102.

## Deployment recovery instructions

If this ChatGPT conversation loses the MCP Devbox namespace after deployment:

1. Do not classify the server as failed from the missing namespace alone.
2. Read this file and Brain note `mcp-devbox-redeploy-continuity` from a new MCP Devbox session or another chat.
3. Verify Coolify app `jqf7qz5ensoqtvl1tb197gcv` only.
4. Verify public `/version` reports the exact merge commit, 102 tools and catalog `sha256:1d3646af205ec2b1a01a47d034641ac4cb8a4843d9c7879b122432308e961007`.
5. Verify unauthenticated `/mcp` and `/console` remain protected.
6. Use an independent MCP client with the existing URL and credential to run `initialize`, `tools/list` and `brain_read`.
7. Test an old-session fixture and require HTTP 404 without tool authority.
8. Classify separately: server recovered/client recovered, server recovered/client stale, server failure, or insufficient evidence.
9. Never change domain, OAuth or connector configuration merely to clear a client cache unless registration is actually invalid.

## Remaining exact sequence

1. Commit and publish this continuity-only update.
2. Wait for all checks on the resulting exact head.
3. Correct only demonstrated CI failures and rerun checks if necessary.
4. Update Brain externally with final head/check results and merge expectation.
5. Preview and execute merge commit.
6. Fetch and fast-forward local `main`; confirm clean.
7. Record pre-deploy production commit and create a normal deployment plan for app `jqf7qz5ensoqtvl1tb197gcv`.
8. Deploy normally, observe readiness/drain and verify production.
9. Update Brain final state and return the complete report.

## Authority confirmation

No general shell was added. OAuth, `/mcp`, `/console`, jails, secrets, plans, grants, audit, Edge and workcells were not weakened. Sessions from a replaced instance no longer retain authority.
