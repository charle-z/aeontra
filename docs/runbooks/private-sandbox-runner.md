# Private L3 sandbox runner

Status: **implementation candidate; Linux real-host and production acceptance pending**

The private runner is a separate service. The public MCP holds only its internal URL,
shared bearer, opaque workspace identifier and expected image digest. Only the runner
may access one validated rootless Podman socket. Neither the public MCP nor a launched
workcell receives that socket.

## Build inputs

Build `Dockerfile.sandbox-workcell`, publish it to an administrator-controlled registry,
and record the immutable `name@sha256:<digest>` identity. Build
`Dockerfile.sandbox-runner` from the same reviewed source commit. A tag without a digest
is rejected.

## Private runner settings

The runner requires:

```text
MCP_DEVBOX_SANDBOX_RUNNER_ADDR=mcp-sandbox-runner:8770
MCP_DEVBOX_SANDBOX_RUNNER_TOKEN=<shared random secret>
MCP_DEVBOX_SANDBOX_WORKSPACE_ID=primary
MCP_DEVBOX_SANDBOX_RUNNER_WORKSPACE_ROOT=<host-visible registered workspace>
MCP_DEVBOX_SANDBOX_RUNNER_STATE_ROOT=<private persistent state outside workspace>
MCP_DEVBOX_SANDBOX_IMAGE=<immutable image@sha256:digest>
MCP_DEVBOX_SANDBOX_RUNNER_PODMAN_SOCKET=/run/user/<runner-uid>/podman/podman.sock
```

Optional positive maxima are `MCP_DEVBOX_SANDBOX_MAX_TIMEOUT_MS`,
`MCP_DEVBOX_SANDBOX_MAX_CPU_MILLIS`, `MCP_DEVBOX_SANDBOX_MAX_MEMORY_MIB`,
`MCP_DEVBOX_SANDBOX_MAX_PROCESSES`, and `MCP_DEVBOX_SANDBOX_MAX_OUTPUT_BYTES`.
`MCP_DEVBOX_SANDBOX_MAX_CONCURRENT` caps admitted containers and defaults to two.
The state root must persist, remain private, and never overlap the writable workspace.
Do not assign this service a public domain or published host port.

The runner talks directly to the bounded Podman v5 API over the validated Unix socket;
it does not package a container-engine CLI. The runner image defaults to UID/GID 10001.
When it is containerized, provision the
dedicated rootless Podman account with that identity or override the container user to
the exact non-root UID/GID that owns the socket. Mount the socket, registered workspace
and private state root at their exact host-visible paths. Do not mount a rootful Docker
socket.

## Public MCP settings

```text
MCP_DEVBOX_SANDBOX=private-rootless
MCP_DEVBOX_SANDBOX_RUNNER_URL=http://<private-service>:8770
MCP_DEVBOX_SANDBOX_RUNNER_TOKEN=<same secret>
MCP_DEVBOX_SANDBOX_WORKSPACE_ID=primary
MCP_DEVBOX_SANDBOX_IMAGE=<same immutable image@sha256:digest>
```

No change to `MCP_DEVBOX_ALLOW_CMD` is needed. That list continues to govern only L1
`run_command`. `read-only` denies `sandbox_exec`; `ask` requires `approve=true`; and
`allow` runs without that mode prompt after the runner reattests.

## Acceptance

Before enabling production, verify on the intended Linux host:

1. `sandbox_status` reports `available:true`, `free_terminal:true`, backend
   `rootless-podman`, and default egress deny.
2. direct argv and an explicitly requested `bash -lc` command both run.
3. metadata, Internet, RFC1918 and loopback egress fail.
4. host files, credential stores, engine sockets and paths outside the workspace are
   absent.
5. secret-named workspace files reject the run before container creation.
6. rootfs writes, capability acquisition, PID/IPC sharing, fork/memory pressure and
   output expansion remain bounded.
7. timeout removes the complete container and a lost response replays the completed
   receipt without repeating the command.

Rollback is configuration-only: set `MCP_DEVBOX_SANDBOX=none` and redeploy the public
MCP. Then stop the private runner. L1 tools, Edge execution, OAuth and Front Door do not
depend on L3.
