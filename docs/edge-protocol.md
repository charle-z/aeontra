# Edge protocol

Status: identity and pairing implemented; task leasing and the WSL workcell are
not implemented yet.

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

## Persistent state

Identity state is stored at `/state/edge/edge.db` with directory mode `0700` and
database mode `0600`. The SQLite store uses full synchronization, a single
connection, and a bounded 8192-page maximum. Symlink roots or ancestors are
rejected.

## Not yet available

Pairing proves device identity only. It does not yet authorize work. Heartbeats,
leases, idempotent tasks, cancellation, reconnection, and the `development`
workcell are the next protocol steps. Do not pair a real WSL device until those
steps and their adversarial tests are complete.
