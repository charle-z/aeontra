# P2 capability service split

Status: in progress on branch `p2-capability-services`.

Completed:
- Step 54 `2ef5414`: central service core and five capability services.
- Step 55 `b9556a3`: shared jailed workdir resolution moved to the core.
- Step 56 `0992d46`: repository/filesystem/memory/notes methods moved to `RepositoryCapability`.
- Step 57 `d76a9a0`: Git methods moved to `GitCapability`.
- Step 58 `570f042`: GitHub/source-hosting methods moved to `SourceCapability`.
- Step 59 `6056b5a`: Coolify/platform methods moved to `PlatformCapability`.

Current Step 60 candidate:
- `ExecutionCapability` now owns allowlisted command execution, configured tests, sandbox execution/status, private fixed-profile validation, and administrator-defined privileged profiles;
- compile-time assertions prove it implements the execution, validation, and privileged catalog interfaces;
- it shares the central policy/audit/root/runner/plan core and retains the existing sandbox, validation-runner, test-command, and privileged configuration dependencies;
- no command allowlist, sandbox posture, approval path, or privileged profile changed.

Step 60 verification:
- RED failed on all three missing execution catalog interfaces;
- focused and full tests passed after receiver migration;
- `go vet ./...` and `go build ./...` passed;
- production catalog smoke remains 62 tools with the unchanged hash.

Next autonomous step: enforce the capability boundary so only composition/configuration methods remain on `Service`, then remove any residual coupling and run the P2 closure audit. Do not publish, merge, or deploy P2 without explicit owner approval.
