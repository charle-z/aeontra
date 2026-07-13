# P5 deeper testing

Status: in progress on branch `p5-deeper-testing` from deployed `main` commit `4a96307925751cf7fbe7a4f8eb801f86c8edc3ad`.

Completed:
- Step 78 `47033fa`: defined P5 and recorded the verified P4 deployment.
- Step 79 `58146f9`: documented the CGO-blocked race baseline and assigned the real gate to P6.
- Step 80 `9bf0f1a`: added deterministic concurrency evidence for grants, plans, audit, and OAuth.

Current Step 81 candidate — fuzz seeds:
- added jail containment, command-policy, redaction-idempotence, and grant-TTL fuzz targets;
- added JSON-RPC message/batch validity and action-plan binding/single-use fuzz targets;
- all targets use curated, secret-free seeds and no network or command execution;
- regular `go test` executes the seed corpus; timed fuzz runs and crash-corpus retention remain P6 work;
- seed execution corrected two test assumptions without requiring runtime changes;
- marked P5 T04 and T05 complete and synchronized testing docs, capsule, roadmap, and handoff.

Step 81 verification so far:
- focused fuzz seed execution passes across policy, mcpserver, and tools;
- remaining gates: full tests, vet, build, diff review/check, and commit.

Next: Step 82 tested package-specific coverage gate. No public MCP contract change.
