# P5 deeper testing

Status: in progress on branch `p5-deeper-testing` from deployed `main` commit `4a96307925751cf7fbe7a4f8eb801f86c8edc3ad`.

Completed:
- Step 78 `47033fa`: defined P5, recorded P4 deployment, synchronized documentation, and froze the public MCP contract.
- Step 79 `58146f9`: recorded the honest race-detector baseline; production builder has CGO disabled and P6 must execute the real race gate.

Current Step 80 candidate — deterministic concurrency:
- added bounded concurrent duplicate-request and single-consume tests for access grants;
- added concurrent single-use action-plan execution with one allowed result and replay denials;
- added 128-writer audit JSONL integrity/redaction coverage;
- added OAuth authorization-code/refresh exactly-once and access-token put/get concurrency tests;
- current mutex boundaries passed the new tests immediately; no runtime code change or race-free claim was made;
- marked T03 complete and synchronized testing docs, capsule, roadmap, and handoff.

Step 80 verification so far:
- focused concurrency tests pass across policy, tools, audit, and OAuth;
- remaining gates: full tests, vet, build, diff review/check, and commit.

Next: Step 81 policy and protocol fuzz targets with curated safe seeds. The actual race detector remains pending P6 in a CGO-enabled environment.
