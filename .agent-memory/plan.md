# Plan — P12 Trusted Linux Workcell final closure

1. Preserve the one-profile contract: linux-workcell, default dev, optional local htb-linux; keep P11.2 sandbox networkless and fail-closed.
2. Finish the rootless lifecycle correction without expanding scope: whole-process-group cancellation, pod cleanup, unique per-run identities, two consecutive clean cycles, condition-based readiness, restart/orphan proof and EXIT-trap cleanup.
3. Run format, focused suites, tagged builds, full serial tests, coverage gate, vet, build, Staticcheck, Govulncheck, Actionlint and git diff --check.
4. Confirm no temporary helpers, scratch, logs, reports, private artifacts or binaries remain; commit the correction and publish only p12-trusted-linux-workcell.
5. Re-run the blocking matrix for the memory synchronization head and require every check green simultaneously with evidence complete; implementation head `d37c991` already demonstrated 15/15 green.
6. Correct only evidenced failures. Do not add hidden retries, continue-on-error, skips, relaxed assertions, arbitrary sleeps, rootful Docker or global serialization.
7. Revalidate and merge PR #25 using a merge commit only; do not squash, rebase or rewrite history.
8. Observe the existing automatic Coolify deployment for jqf7qz5ensoqtvl1tb197gcv. Trigger one normal non-force deployment only if no webhook deployment appears after bounded observation and production remains on the old commit.
9. Verify the merge commit in healthy production with 85 tools, the unchanged catalog hash, catalog/Brain/console/OAuth/session/Graph/Lifetime/P11.2 smokes, no Edge requirement and not_paired state.
10. Report the exact rootless cause, commits, final SHA/tree, checks, merge, deployment, production, shared-network risk, Parrot human setup and residual risks. Do not modify real Parrot or open later milestones.
