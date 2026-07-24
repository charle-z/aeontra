# Current task

Historical deployed baseline remains unchanged: VPS `main`/production truth is preserved separately; no production, Coolify, Parrot or `main` mutation occurred in this cut.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Published base before this local cut: `16a037f1fc23a78aeb4d4c1df99155725e1cf7e2`.

## Exact-head diagnosis

The previous head completed 14/16 checks green. The two failures were both fixture-level:

- `Verify` failed only because ShellCheck rejected a `sudo ... > file` redirection in the BuildKit workflow.
- `Rootless BuildKit candidate fixture` again completed both OCI builds and emitted `CACHED`; the failure occurred only while trying to traverse the exported local-cache directory. The uploaded artifact proved no cache-inventory file was produced.

The rootless service, dedicated cgroup subtree, stop behavior, PostgreSQL/Podman fixture and every other gate remained healthy.

## Local Step 7 candidate

The BuildKit fixture now uses the daemon's own supported cache inspection command:

- `buildctl --addr <private-socket> du -v` executes as `mcp-build`;
- output is retained as `cache-usage.txt`, must be non-empty and is bounded to 1 MiB;
- the installed configuration must contain the reviewed `maxUsedSpace = "4GB"` policy and is retained as `cache-policy.txt`;
- cache reuse is still proved independently by the second solve's `CACHED` result.

This removes direct filesystem traversal of rootless cache state and removes the ShellCheck redirection problem.

A new fixed `packaging/builder/bootstrap-vps.sh` reduces the unavoidable host administrator boundary to one exact-commit invocation. It:

- accepts only one lowercase 40-character SHA for the fixed public owner repository;
- validates a root-owned non-writable entrypoint;
- reexecutes itself under the fixed transient `mcp-devbox-builder-bootstrap.service` with a four-hour runtime cap, so work survives SSH disconnects;
- starts the unit from an empty environment so tokens, proxies and shell state are not inherited;
- uses a private lock and exact clean detached checkout with Git credential/config disabled;
- stages the pinned BuildKit release and invokes only the fixed install/calibration/removal scripts;
- reuses an existing builder only when binaries, config and unit match byte-for-byte and the service is active;
- rejects partial or different installations without overwriting them;
- removes only a candidate created by that attempt if calibration fails, while preserving private evidence and staging.

The deployed public MCP container cannot perform this host mutation: it is non-root UID 10001, has no host systemd and intentionally has no host filesystem or Docker socket. Real VPS execution therefore remains a genuine human root boundary after exact-head CI is green.

## Validation completed locally

Green on the exact local tree:

- all Go packages in bounded groups;
- focused package, workflow and documentation tests;
- `go vet ./...`;
- Staticcheck v0.7.0;
- `go build ./...`;
- Actionlint v1.7.12;
- `git diff --check`;
- bootstrap shell syntax/executable/authority tests;
- no temporary helper or diagnostic files remain.

Next: commit and publish this Step 7 cut, hold the SHA stable and require every exact-head gate green. Only then provide the single checksummed root bootstrap command for the VPS, run the real 50/65/80 calibration, collect the private evidence and create a dated baseline before Step 8.
