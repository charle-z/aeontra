# P6 CI/DevSecOps

Status: in progress on branch `p6-ci-devsecops` from deployed `main` commit `4a68ca054a5f077d62a0f887234866673feb7353`.

P5 release verification:
- feature branch published and `main` advanced by fast-forward only;
- Coolify deployment `tsrkggf8hf5aaq3c40qy9lv5` finished successfully;
- production runtime commit is `4a68ca054a5f077d62a0f887234866673feb7353`;
- application is healthy;
- catalog remains 62 tools with hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

Current Step 85 candidate — P6 foundation:
- created `specs/003-ci-devsecops/spec.md`, `plan.md`, and `tasks.md`;
- scope covers workflow policy, core CI, CGO race, static/vulnerability analysis, CodeQL/dependency review, container evidence, and bounded scheduled fuzz;
- explicitly excludes PR deployment, production DAST, repository secrets in fork-facing jobs, runtime authority, and public MCP changes;
- transitioned capsule, roadmap, README, AGENTS, Layer-1 follow-on tasks, P5 assertions, documentation-state assertions, and handoff from merge-ready P5 to deployed P5 / active P6;
- historical P5 closure baseline remains unchanged;
- T01 is marked complete.

Step 85 verification:
- RED failed because P6 had no specification or active state;
- focused and full documentation/tests pass;
- package coverage gate, `go vet ./...`, `go build ./...`, and formatting pass;
- remaining: cleanup temporary helpers, diff review/check, stage, and commit.

Next: Step 86 tested workflow policy guard before modifying GitHub Actions. Do not publish, merge, or deploy P6 until observed Actions and phase closure.
