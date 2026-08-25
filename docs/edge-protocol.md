# Edge protocol

Status: identity, pairing, and leased task transport implemented; the WSL
`development` workcell is not implemented yet.

## Boundary

Edge extends mcp-devbox to a separately installed workcell without exposing a
remote shell. The workcell initiates every connection from WSL to the VPS. It
must not mount Windows drives, a Docker socket, or privileged host paths, and it
must run as a dedicated non-root user.

The Edge HTTP surface is separate from MCP and OAuth:

- MCP remains at `/mcp` with its existing bearer/OAuth authentication.
- Edge pairing is `POST /edge/v1/pair`.
- Later Edge task routes require a paired device signature.
- Pairing does not grant MCP access and MCP credentials do not authenticate an
  Edge device.

## One-time pairing

An operator creates a random one-time code in the private state volume:

```bash
mcp-devbox edge pairing-create --state-root /state --ttl 10m
```

The code is printed once to that operator terminal. Only its SHA-256 digest is
stored. Its lifetime cannot exceed ten minutes and a successful exchange consumes
it atomically.

The client generates its own Ed25519 key pair and sends a JSON body no larger
than 4 KiB:

```json
{
  "code": "ep_<opaque>",
  "name": "wsl-development",
  "public_key": "<base64url Ed25519 public key>"
}
```

The response contains only `device_id`, `name`, `state`, and `paired_at`. It never
echoes the pairing code or public key. The private key never leaves the device.

An operator can revoke the returned device id immediately:

```bash
mcp-devbox edge revoke --state-root /state --device ed_<opaque>
```

## Device request authentication

Authenticated Edge requests use these headers:

- `X-Edge-Device`
- `X-Edge-Timestamp` (Unix seconds)
- `X-Edge-Nonce` (16–96 base64url-safe characters)
- `X-Edge-Signature` (raw base64url Ed25519 signature)

The exact signed bytes are:

```text
edge-v1\n
<device_id>\n
<unix_timestamp>\n
<nonce>\n
<UPPERCASE_METHOD>\n
<escaped_path>\n
<lowercase_sha256_hex_of_exact_body>
```

The server permits two minutes of clock skew and persists each accepted nonce
for ten minutes. A nonce is consumed in the same database transaction as
authentication, so replay is rejected across requests and process restarts.
Bodies on signed routes are capped at 1 MiB before signature verification.

## Leased task transport

The VPS stores only bounded, structured tasks for the single `development`
workcell. It never sends shell text or an argv array. A task contains:

- an operator-chosen idempotency key;
- a `validate` objective kind, short summary, and at most eight acceptance criteria;
- a workcell-local workspace name, never an absolute path;
- `none` or reserved `registry` network policy;
- duration (30–3600 seconds) and result-size (1 KiB–1 MiB) limits.

Objective and acceptance text that matches the central secret scanner is rejected.
Completion summaries pass through the same scanner before persistence. Task state
shares the bounded Edge SQLite database.

The signed worker routes are:

```text
POST /edge/v1/tasks/lease
POST /edge/v1/tasks/<task_id>/heartbeat
POST /edge/v1/tasks/<task_id>/complete
```

A lease lasts 15 seconds to 10 minutes. Repeating a lease request with the same
device and holder returns the same unexpired lease. Another holder cannot receive
that task concurrently. Once expired, the same task and idempotency key may be
redelivered with a new lease id and incremented attempt. The WSL agent journals
that idempotency key locally before execution and replays a previously completed
result instead of executing twice. If it finds a `started` entry after a crash, it
fails closed for manual reconciliation rather than rerun.

Heartbeat extends only the exact active lease and reports `cancel_requested`.
Completion requires the exact current lease. Repeating the identical completion is
safe; a changed terminal result or stale lease is rejected. Cancelling a queued task
makes it terminal immediately. Cancelling a leased task is observed on heartbeat and
the worker acknowledges it with a `cancelled` completion.

There is deliberately no general MCP orchestration tool in this step. An operator
can inspect devices and enqueue, inspect, or cancel a structured task from the
private runtime terminal:

```bash
mcp-devbox edge devices --state-root /state
mcp-devbox edge task-create --state-root /state \
  --device ed_<opaque> \
  --idempotency portfolio-check-0001 \
  --workspace portfolio-charlez \
  --objective "validate the checked-out project" \
  --accept "checks pass" \
  --network none \
  --max-duration 10m \
  --max-output 262144
mcp-devbox edge task-status --state-root /state --task et_<opaque>
mcp-devbox edge task-cancel --state-root /state --task et_<opaque>
```

## Persistent state

Identity state is stored at `/state/edge/edge.db` with directory mode `0700` and
database mode `0600`. The SQLite store uses full synchronization, a single
connection, and a bounded 8192-page maximum. Symlink roots or ancestors are
rejected.

The operation journal keeps a storage reserve of one sixteenth of that page budget,
capped at 512 pages. When the reserve is exhausted, the server deletes the oldest
terminal `succeeded`, `failed`, or `cancelled` operations in bounded batches. Queued
and leased operations are never reclaimed. Terminal operation IDs are durable
coordination handles, not permanent audit records; the separate bounded audit and
observability logs remain the historical evidence sources.

## WSL development workcell

`cmd/mcp-edge` is the separately installed outbound client. Pairing reads the code
only from stdin, generates the device key locally, and persists private state with
`0700`/`0600` permissions. The runner refuses root, non-Linux systems, `/`, `/mnt`,
symlinked workspaces, overlapping private/workspace roots, missing Bubblewrap, and
unsupported network policy.

The local validation planner recognizes project markers rather than accepting a
remote command: Go, locked pnpm/npm, Python, and Rust. Repository scripts execute
only inside Bubblewrap with `--unshare-all`, a single writable `/workspace`,
read-only runtime paths, ephemeral `/tmp`, no home, no Edge identity, no Windows
mount, and no Docker socket. Output is bounded and redacted before its compact
summary is returned.

The local SQLite journal records `started` synchronously before execution and
`completed` before delivery. Lost completion delivery is replayed without another
execution. A crash after start fails closed instead of guessing. Heartbeat observes
server cancellation; `/var/lib/mcp-devbox-edge/STOP` is the independent local kill
switch. See `docs/install-edge-wsl.md` for the human-only installation and pairing
procedure.

## Deliberately deferred

The initial workcell validates existing source; it is not a general coding-agent
or remote shell. Registry-only egress, local OpenCode/provider adapters, mutation
authority, Parrot, Burp, HTB, and security-engagement profiles require separate
reviewed policy layers. They are not implied by pairing this device.
