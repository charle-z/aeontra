# P5 deeper testing

Status: in progress on branch `p5-deeper-testing` from deployed `main` commit `4a96307925751cf7fbe7a4f8eb801f86c8edc3ad`.

Completed:
- Step 78 `47033fa`: defined P5 deeper testing, recorded the verified P4 deployment, synchronized documentation, and froze the public MCP contract.

Current Step 79 candidate — race baseline:
- attempted `go test -race ./... -count=1` on the deployed-builder environment;
- the command stopped before tests executed because `CGO_ENABLED=0`;
- verified environment: linux/amd64, `CC=gcc`, `CGO_ENABLED=0`;
- did not mutate persistent `go env`, weaken production build configuration, or claim a green race result;
- added `docs/testing.md` with the canonical `CGO_ENABLED=1 go test -race ./... -count=1` command, observed result, safety rules, and the requirement for P6 to run the real blocking gate in an ephemeral CGO-enabled environment;
- marked P5 T02 complete as a documented prerequisite/result, not as a passed race suite;
- synchronized quality gates, README, AGENTS, capsule, roadmap, and handoff.

Step 79 verification:
- RED documentation test failed because no honest race baseline existed;
- focused documentation tests pass after recording the blocked status;
- ordinary P5 baseline remains green from Step 78; final Step 79 gates are full tests, vet, build, diff review/check, and commit.

Next: Step 80 deterministic concurrency stress around actual shared-state stores. The P6 branch must later execute and retain the real race-detector result with CGO enabled. Do not publish, merge, or deploy P5 until phase closure.
