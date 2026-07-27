# Current task — Step 17 documentation cleanup

Step 16 documentation foundation is merged into `main` at merge commit `fe19479c5022a0836426c9553c403b9833f2a772` through PR #66.

Branch: `step17-documentation-cleanup`, based on that clean synchronized merge commit.

## Scope completed locally

- converted `AGENTS.md` into stable operating rules rather than a release diary;
- reduced `docs/context-capsule.md` from a phase history to bounded continuation context;
- classified `docs/features.md` and `docs/design.md` as historical/conceptual sources;
- made `docs/connect-remote.md` and `docs/deploy-coolify.md` delegate full configuration and tool truth to their canonical references;
- marked the P15 Parrot guide historical and the P16 guide canonical while separating source, package, VPS and installed-device evidence;
- migrated historical documentation contracts to baselines, specs, ADRs, roadmap and runbooks instead of requiring commits, hashes and tool counts in current operational docs;
- restored visible CubePath attribution in `README.md` and recorded that MCP Devbox remains an active Cubethon 2026 Q3 hackathon entry without mixing event context with live runtime identity;
- added `docs/step17_documentation_cleanup_test.go` and updated affected documentation regressions.

No runtime implementation, public schema, route, OAuth behavior, CSP, authority model, Edge package, service, BuildKit, AppArmor, runner, quota or deployment configuration changed.

## Validation

- deliberate Step 17 documentation contract: passed;
- `go test ./docs -count=1`: passed after final review;
- all Go packages passed: the monolithic command was terminated by the tool execution limit after green results through `internal/resultstore`; the remaining packages were then run explicitly and all passed;
- `go vet ./...`: passed after final review;
- `go build ./...`: passed after final review;
- `git diff --check`: passed after final review;
- all relative links in changed operational documents resolve;
- complete diff reviewed; one over-removed static constitution check was restored.

## Remaining actions

1. Create the Step 17 commit with author and committer `mcp-devbox-edge <edge@mcp-devbox.local>` and verify the exact object before publication.
2. Publish the branch and open a normal PR.
3. Require every exact-head CI gate green before merge.
4. Merge with a merge commit, synchronize clean local `main`, and observe the automatic Coolify deployment.
5. Verify the live exact commit without triggering an unnecessary manual/no-cache deployment.
