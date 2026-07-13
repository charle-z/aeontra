# P5 deeper testing

Status: in progress on branch `p5-deeper-testing` from deployed `main` commit `4a96307925751cf7fbe7a4f8eb801f86c8edc3ad`.

Completed:
- Step 78 `47033fa`: defined P5 and recorded the verified P4 deployment.
- Step 79 `58146f9`: documented the CGO-blocked race baseline and assigned the real gate to P6.
- Step 80 `9bf0f1a`: added deterministic concurrency evidence for grants, plans, audit, and OAuth.
- Step 81 `adf4e02`: added curated safe fuzz targets for policy, JSON-RPC, grants, and plans.
- Step 82 `350429b`: added the package-specific coverage gate and passing security-package thresholds.

Current Step 83 candidate — hermetic integration matrix:
- added `internal/integration/contracts_test.go` with loopback/temp-state contracts for stdio/HTTP catalog parity, bearer fail-closed behavior, OAuth challenge metadata, runtime identity, local grant approval and single-use redacted reads, and planned note approval/execution/replay denial;
- no external credentials, production traffic, arbitrary commands, or non-loopback services are used;
- added a documentation contract test and recorded the matrix in `docs/testing.md`;
- marked P5 T07 complete and synchronized capsule, roadmap, and latest handoff.

Step 83 verification so far:
- RED failed because no integration matrix or documentation existed;
- focused integration and documentation suites pass;
- remaining: full tests, coverage gate, vet, build, diff review/check, cleanup, and commit.

Next: Step 84 P5 closure baseline and branch audit. Race-detector execution and timed fuzz remain P6 work. No public MCP contract change.
