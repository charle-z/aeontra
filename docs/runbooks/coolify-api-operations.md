# Coolify API operations runbook

## Purpose

MCP Devbox is intended to administer the configured Coolify instance through its API,
not merely deploy applications that were prepared manually. A missing public MCP tool
or wrapper is an implementation gap; it is not evidence that Coolify requires a human
panel action.

Before asking the operator to change Coolify manually:

1. inspect the current application and configured platform capabilities;
2. inspect the official Coolify v4 API operation for the resource;
3. check whether the existing fixed-base-URL authenticated client can call it safely;
4. distinguish missing MCP support from missing token authority;
5. prefer a narrow planned/idempotent operation over Docker socket, SSH, shell, or a
   new application.

The repository and this runbook are the source of truth. Do not depend on another chat
remembering these endpoints.

## Existing authority model

The internal Coolify client owns a fixed administrator-configured base URL and API
token. Agent input cannot replace the host or receive the token. Application UUIDs are
validated and optional `COOLIFY_ALLOWED_APPS` enforcement occurs in the MCP service.

When `COOLIFY_ALLOWED_APPS` is absent or empty, any structurally valid application UUID
on the configured Coolify instance may be managed. This is intentional global-builder
behavior. Set the variable only when the administrator wants an application allowlist.

A token capability must be proved by the actual endpoint response:

- successful reads prove `read` authority for that resource;
- a non-auth application error such as `409` proves the request authenticated and
  reached the operation;
- only `401` or `403` justify asking the operator to change token permissions;
- never request or rotate a broader token pre-emptively.

## Application storage API

Coolify v4 manages storage for an existing application under:

```text
GET    /api/v1/applications/{uuid}/storages
POST   /api/v1/applications/{uuid}/storages
PATCH  /api/v1/applications/{uuid}/storages
DELETE /api/v1/applications/{uuid}/storages/{storage-uuid}
```

MCP Devbox must not expose the generic destructive surface by default. Product-specific
workflows should use the minimum required methods and closed payloads.

### P9 Brain storage contract

For application `jqf7qz5ensoqtvl1tb197gcv`, the only accepted creation payload is:

```json
{
  "type": "persistent",
  "name": "mcp-devbox-brain",
  "mount_path": "/brain"
}
```

Omit `host_path`. This requests a Coolify/Docker-managed persistent volume instead of
an arbitrary host bind mount.

The safe operation is `ensure`, not blind create:

1. `GET` current storages.
2. If one entry has type `persistent`, name `mcp-devbox-brain`, and mount path
   `/brain`, return success without mutation.
3. If the name or mount path collides with a different definition, fail closed and
   report the sanitized conflict.
4. Otherwise `POST` the exact closed payload.
5. `GET` again and require the exact resulting definition.
6. Never call storage `DELETE` during P9 closure.
7. Do not deploy or set `MCP_DEVBOX_BRAIN_ROOT` until the verified storage exists.

The workflow must not accept arbitrary application IDs, volume names, mount paths,
host paths, types, URLs, tokens, or HTTP methods when used for P9 closure.

## Environment variable create versus update

Coolify distinguishes creation from replacement:

- create a missing application environment variable with `POST`;
- update an existing variable with the supported `PATCH` operation;
- a `409` saying the variable already exists is not a permission failure.

A helper named `set` or `upsert` must inspect current state or handle the documented
conflict safely, then choose create or update. It must not claim replacement support
while implementing only `POST`.

Values are secrets or private configuration:

- send them only in authenticated request bodies;
- never return, log, persist in audit arguments, or write them into repository files;
- report only variable names, operation type, HTTP status class, and sanitized result.

For P9, set this only after `/brain` has been verified:

```text
MCP_DEVBOX_BRAIN_ROOT=/brain
```

## What is not required

Storage management through the Coolify API does not require:

- `/var/run/docker.sock` in the public MCP container;
- SSH or a free host terminal;
- a second Coolify token unless the endpoint returns `401` or `403`;
- `MCP_DEVBOX_MODE=allow`;
- a new Coolify application;
- `COOLIFY_ALLOWED_MOUNTS`, which is currently dedicated to the validation-runner
  creation contract;
- privileged task profiles when a narrow API workflow can perform the operation.

## Deployment sequencing

For storage-dependent releases:

1. verify source branch, merge commit, checks, and existing application identity;
2. ensure the required persistent storage through the API;
3. verify storage by a fresh read;
4. upsert the dependent environment variable;
5. create one deployment preview bound to the exact application/repository/branch/
   commit;
6. execute once and retain the returned deployment UUID;
7. observe that same deployment to terminal state;
8. verify health, runtime commit, tool count, catalog hash, logs, and product smoke;
9. do not create a second deployment merely because the MCP client disconnected;
10. tag only after production verification.

## Failure classification

| Result | Meaning | Action |
|---|---|---|
| `2xx` | Operation accepted/succeeded | Verify by reading state again. |
| `409` | Existing/conflicting resource or wrong create/update method | Inspect state; use idempotent success or the correct update operation. |
| `400`/`422` | Payload or API contract mismatch | Correct implementation; do not ask for broader authority. |
| `401` | Token absent/invalid | Ask operator to repair authentication. |
| `403` | Token authenticated but lacks required ability | Ask operator for the exact missing ability only. |
| `5xx` | Coolify/server failure | Preserve current healthy deployment and retry only under bounded policy. |

## Documentation rule

When adding a new Coolify operation, update this runbook with:

- endpoint and method;
- closed request contract;
- token ability required;
- idempotency and conflict behavior;
- destructive methods deliberately excluded;
- redaction requirements;
- production verification and rollback.

Do not let operational knowledge exist only in chat history.
