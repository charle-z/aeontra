# Private pnpm validation runner

The public `mcp-devbox` container must **not** receive a Docker socket, a host
shell, or a generic Node/npm command allowlist. Those would let package scripts
escape the intended review boundary. JavaScript validation instead runs through a
separate private service, `validation-runner`.

## Fixed profiles

The runner accepts only a direct repository name under its configured `/repos`
root and one of these names:

| Profile | Effect | Network |
| --- | --- | --- |
| `pnpm-lockfile` | Pin pnpm 10.13.1, generate `pnpm-lock.yaml`, then prefetch that resolved graph. Lifecycle scripts are disabled. | Docker bridge, only for dependency resolution and fetching. The child has no MCP, Coolify or GitHub credentials. |
| `pnpm-validate` | Offline frozen install with lifecycle scripts disabled, then fixed `pnpm run check`, `pnpm test`, and `pnpm run build`. | None. |

Both child containers are ephemeral and run with a read-only root filesystem,
no capabilities, `no-new-privileges`, resource limits, a temporary `/tmp`, a
single repository mount and a runner-owned pnpm cache volume. The project scripts
in the validation profile are code execution, but they execute only in that
contained child with no host socket and no network.

The profiles intentionally invoke `corepack pnpm`, not `corepack enable`: enabling
would try to write package-manager shims into the read-only image filesystem. The
networked profile also creates a pinned Corepack archive in the runner-owned volume
and installs it into `COREPACK_HOME`. The offline profile refuses to start unless
that archive exists, installs it with `--cache-only`, and sets
`COREPACK_ENABLE_NETWORK=0` before any project command runs.

The `pnpm-lockfile` profile intentionally has a narrow network exception. A
package manifest can name dependencies fetched from third parties, so this is a
reviewed supply-chain action rather than a harmless local check. It never runs
package lifecycle scripts and its output is bounded and redacted.

## One-time VPS deployment

Build the runner image on the Docker host from a clean checkout of the same
repository. `/repos/mcp-devbox` is inside the public container, not necessarily a
host path; clone it to an administrator-only directory on the VPS instead:

```bash
git clone https://github.com/charle-z/aeontra.git /opt/mcp-devbox-runner
cd /opt/mcp-devbox-runner
git checkout <the-main-commit-being-deployed>
docker build -f Dockerfile.validation-runner -t mcp-devbox-validation-runner:local .
```

Create a long random token locally on the VPS. Store it only as a Coolify secret
for the MCP service and as an environment variable for this private runner; never
put it in a repo, prompt, log or `docker inspect` output.

Run the runner with no published ports, on the private `coolify` network, and
with the Docker socket. `MCP_DEVBOX_VALIDATION_RUNNER_ROOT` is the path inside the
runner container. `MCP_DEVBOX_VALIDATION_RUNNER_HOST_ROOT` is the physical path
Docker uses on the VPS; they are deliberately separate for Docker named volumes.

```bash
docker volume create mcp-devbox-pnpm-store
docker run -d --name mcp-devbox-validation-runner --restart unless-stopped \
  --network coolify \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /HOST/PATH/TO/REPOS:/repos \
  -v mcp-devbox-pnpm-store:/pnpm-store \
  -e MCP_DEVBOX_VALIDATION_RUNNER_ROOT=/repos \
  -e MCP_DEVBOX_VALIDATION_RUNNER_HOST_ROOT=/HOST/PATH/TO/REPOS \
  -e MCP_DEVBOX_VALIDATION_RUNNER_TOKEN='REPLACE_WITH_SECRET' \
  mcp-devbox-validation-runner:local
```

The UID in `/repos` must be writable by UID `10001`; if the existing volume uses
another non-root UID, set `MCP_DEVBOX_VALIDATION_RUNNER_USER` to that numeric
`uid:gid`. Do not use root for child validation jobs.

Then add only these two secrets to the **MCP Devbox** Coolify application and
redeploy it:

```text
MCP_DEVBOX_VALIDATION_RUNNER_URL=http://mcp-devbox-validation-runner:8787
MCP_DEVBOX_VALIDATION_RUNNER_TOKEN=<same secret>
```

No public domain, port mapping, Traefik route or ChatGPT connector change is
needed for the runner. It is reachable only from the MCP container on the Docker
network. Check its health from the VPS, not from the Internet:

```bash
docker exec mcp-devbox-validation-runner wget -qO- http://127.0.0.1:8787/healthz
```

## ChatGPT workflow after activation

1. Call `project_validation_preview` with `repo: "portfolio-charlez"` and
   `profile: "pnpm-lockfile"`.
2. Review the output and execute the returned plan with
   `project_validation_execute` and `approve: true`.
3. Inspect and commit the resulting real `pnpm-lock.yaml`.
4. Repeat with `profile: "pnpm-validate"` to run the offline check, test and
   build. Only after all pass should publication/deployment continue.

The public MCP’s `run_command`, `run_tests`, `sandbox_exec` and privileged tools
remain intentionally unable to become a general Node terminal.
