# Private L3 sandbox runner

Status: **implementation candidate; Linux real-host and production acceptance pending**

The private runner is a separate service. The public MCP holds only its internal URL,
shared bearer, opaque workspace identifier and expected image digest. Only the runner
may access one validated rootless Podman socket. Neither the public MCP nor a launched
workcell receives that socket.

## Build inputs

After an exact-main pull request is green and merged, dispatch
`.github/workflows/sandbox-image-release.yml` from `main`. Its protected
`sandbox-image-release` environment publishes `ghcr.io/charle-z/aeontra-sandbox-runner`
and `ghcr.io/charle-z/aeontra-sandbox-workcell`, both tagged only with the exact source
commit and retained as immutable `name@sha256:<digest>` identities. The workflow links
the packages to the source repository, emits registry provenance and SBOM attestations,
and retains a bounded JSON identity artifact. A tag without a digest is rejected by the
runner configuration.

New GitHub Container Registry packages are private until an administrator changes their
visibility. Before a credential-free production pull, make both packages public in the
package settings and verify an anonymous digest pull. If they remain private, provision
read-only registry authentication exclusively for the rootless Podman account; never
place registry credentials in the public MCP container or a launched workcell.

The reference workcell uses a digest-pinned Wolfi base and exact package versions for
Go, Rust, Node/npm, Python, the C toolchain and common command-line utilities. Its two
replaced npm transitive packages are fetched over HTTPS and verified by SHA-256. The
security workflow builds this exact image, emits an SPDX SBOM and rejects every current
High or Critical Grype finding. Treat any package-version change as a reviewed image
update and repeat that gate before publishing a new digest.

## Private runner settings

The runner requires:

```text
MCP_DEVBOX_SANDBOX_RUNNER_IPV4=<reserved-private-ip>
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

Reserve one unused address from the external Coolify network in Compose. The reference
Compose derives `MCP_DEVBOX_SANDBOX_RUNNER_ADDR=<reserved-private-ip>:8770` from that
single value and assigns the same IP through IPAM. Coolify may also attach a
service-specific network, so a service hostname can resolve to more than one interface
after recreation. Hostname, wildcard and public listeners are invalid and the Compose
publishes no host port.

## Rootless storage

Use a dedicated rootless graphroot with the kernel overlay driver when the target host
passes a real `keep-id` probe:

```toml
[storage]
driver = "overlay"
runroot = "/run/user/10001/containers"
graphroot = "/var/lib/aeontra-l3-user/storage-native"
```

Do not remove `keep-id` to work around a storage failure. On kernels that support
rootless native overlay, a `fuse-overlayfs` mount program can prevent the nested
namespace runtime from traversing the graphroot. Validate the exact pinned workcell
with `--userns keep-id`, the configured non-root UID/GID, network disabled, a read-only
rootfs, dropped capabilities and no new privileges.

Do not convert a populated graphroot in place. Use these phases:

1. **Preflight:** require zero rootless containers, record the active storage driver and
   graphroot, verify the new graphroot is absent or empty, and run the exact pinned
   workcell under `keep-id` against a separate temporary native-overlay store. A failed
   probe stops the migration.
2. **Prepare:** stop only the rootless Podman service, copy `storage.conf` to a dated
   owner-only backup, retain the old graphroot unchanged, and create a new empty
   owner-only graphroot.
3. **Commit:** install the native-overlay configuration, restart the rootless service,
   pull the exact approved workcell digest, and repeat the `keep-id` probe. The migration
   is committed only after the socket, rootless engine identity and probe are healthy.
4. **Rollback:** stop the rootless service, restore the saved configuration, restart the
   service and verify that it reports the original graphroot and driver. Do not delete
   either graphroot until the new store has passed its operational acceptance period.

Docker, Coolify and the public MCP deployment are outside this migration. A failed
probe, changed engine identity, missing socket, unexpected container or service restart
count is a FAIL and requires rollback before the runner is restarted.

The runner talks directly to the bounded Podman v5 API over the validated Unix socket;
it does not package a container-engine CLI. The runner image defaults to UID/GID 10001.
When it is containerized, provision the
dedicated rootless Podman account with that identity or override the container user to
the exact non-root UID/GID that owns the socket. Mount the socket, registered workspace
and private state root at their exact host-visible paths. Do not mount a rootful Docker
socket.

`deploy/sandbox-runner-compose.yml` is the reference private deployment. It publishes no
host port, reserves one private address on the existing Coolify network, drops every
capability, uses a read-only root filesystem and runs as UID/GID 10001. The three
writable authorities are explicit: the rootless Podman socket, the registered workspace
and the disjoint receipt state. The image healthcheck authenticates to `/v1/status` and becomes healthy
only after the configured image and rootless endpoint attest successfully.

## Public MCP settings

```text
MCP_DEVBOX_SANDBOX=private-rootless
MCP_DEVBOX_SANDBOX_RUNNER_URL=http://<private-service>:8770
MCP_DEVBOX_SANDBOX_RUNNER_TOKEN=<same secret>
MCP_DEVBOX_SANDBOX_WORKSPACE_ID=primary
MCP_DEVBOX_SANDBOX_IMAGE=<same immutable image@sha256:digest>
```

No change to `MCP_DEVBOX_ALLOW_CMD` is needed. That list continues to govern
`run_command` inside L3. Both `read-only` and `ask` deny contained execution. An
administrator must select `allow`, after which the runner still reattests before each run.

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
