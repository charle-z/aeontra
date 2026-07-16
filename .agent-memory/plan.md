# P11.2 execution plan

1. [done] Reconstruct Git, CI and the exact nested-Docker Bubblewrap failure; write the initial checkpoint.
2. [done] Implement closed, redacted Bubblewrap failure stages and unit tests for every required code.
3. [done] Add a fast incremental host preflight covering namespaces, mappings, mounts, binds, socket, network isolation and helper execution.
4. [done] Split P11.2 into an unprivileged Docker relay job and an authoritative Ubuntu 22.04 host Bubblewrap/combined OpenCode job.
5. [done] Remove nested Docker `SYS_ADMIN`, `apparmor=unconfined` and `seccomp=unconfined`; preserve explicit fail-closed negative coverage.
6. [active] Publish the validation tree and obtain green remote evidence for relay container, Bubblewrap host isolation and combined OpenCode sandbox.
7. [pending] Generate and extract final evidence from the green reports; add Parrot WSL documentation and the dated P11.2 baseline/benchmark.
8. [pending] Run the complete final local and remote gate matrix on one exact tree, record `git write-tree`, publish the closure commit, update PR #13 and mark it ready only after all mandatory checks complete green.

Local pre-CI checkpoint:
- `go test -p 1 ./... -count=1` passed;
- `go test -tags=opencode_e2e -p 1 ./... -count=1` passed;
- `go vet ./...` passed;
- `go build ./...` passed;
- `git diff --check` passed;
- Actionlint v1.7.12 passed;
- no temporary helper or compiled artifact remains.

Hard boundaries: no merge, deployment, pairing, real Parrot installation, tag, Coolify changes, frontend, Goal Runtime, Build Workcell, HTB/THM/VPN or broad historical CodeQL cleanup.
