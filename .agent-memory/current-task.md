# P2 capability service split

Status: in progress on branch `p2-capability-services`.

Completed:
- Step 54 `2ef5414`: central service core and five capability services.
- Step 55 `b9556a3`: shared jailed workdir resolution moved to the core.
- Step 56 `0992d46`: repository/filesystem/memory/notes methods moved to `RepositoryCapability`.
- Step 57 `d76a9a0`: Git methods moved to `GitCapability`.
- Step 58 `570f042`: GitHub/source-hosting methods moved to `SourceCapability`.

Current Step 59 candidate:
- `PlatformCapability` now owns legacy Coolify operations, planned application creation/deployment, logs/status, environment mutation, force-without-cache deployment, and managed validation-runner application creation;
- compile-time assertions prove it implements the platform core, deployment, environment, validation-runner platform, and application-preview contracts;
- it shares the central policy/audit/root/plan core and the exact configured `SourceCapability` for GitHub owner validation;
- the aggregate `Service` remains backwards compatible through promoted methods.

Step 59 verification:
- RED failed on all five missing platform contracts;
- focused and full tests passed after receiver migration;
- `go vet ./...` and `go build ./...` passed;
- production catalog smoke remains 62 tools with the unchanged hash.

Next autonomous step: migrate command/test, sandbox, private validation, and privileged profile methods to `ExecutionCapability`. Do not publish, merge, or deploy P2 without explicit owner approval.
