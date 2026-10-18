# P9 Coolify closure

## Scope

This runbook closes the production-only gap after P9 was merged to `main` at
`1faddafd866c426edf5e76d4d336d0b2b7d3f2b6`. It does not change the console,
OAuth, Edge, workcells, HTB, the application count, or the 67-tool public catalog.

Use only the existing Coolify application:

```text
jqf7qz5ensoqtvl1tb197gcv
```

## Persistent Brain storage

The storage operation is a fixed internal helper, not a public tool. It is invoked
only from the existing `coolify_set_env` authority when all three values are exact:

- application `jqf7qz5ensoqtvl1tb197gcv`;
- key `MCP_DEVBOX_BRAIN_ROOT`;
- value `/brain`.

Any other application or Brain root is rejected before an HTTP request.

The operation always lists storages first with:

```text
GET /api/v1/applications/jqf7qz5ensoqtvl1tb197gcv/storages
```

Coolify v4 returns storage lists grouped as `persistent_storages` and
`file_storages`; the client normalizes the collection name into the storage type
before applying the exact-name and exact-mount conflict rules.

Coolify prefixes the physical volume name with the fixed application UUID. The
client removes only that exact platform-owned prefix before comparing the logical
name `mcp-devbox-brain`; type and mount path remain exact and unchanged.

It accepts idempotent success only when exactly one relevant entry has all of:

```json
{"type":"persistent","name":"mcp-devbox-brain","mount_path":"/brain"}
```

A reused name, reused mount path, wrong type, wrong path, or duplicate exact entry is
a conflict and performs no write. When no relevant entry exists, it sends exactly:

```text
POST /api/v1/applications/jqf7qz5ensoqtvl1tb197gcv/storages
```

```json
{"type":"persistent","name":"mcp-devbox-brain","mount_path":"/brain"}
```

`host_path` is omitted so Coolify creates a managed persistent volume. The helper
then lists storages again and fails unless the exact entry is verified. It never calls
DELETE and exposes no arbitrary application, mount, payload, command, terminal, or
Docker socket.

A newly created managed volume may expose an empty root with platform-default mode
`0755`. Brain may harden only that empty root to `0700` before creating any content.
If the root is non-empty, a mode other than `0700` remains a startup failure and no
existing content or permissions are changed.

Execution flow:

```text
coolify_set_env
  app: jqf7qz5ensoqtvl1tb197gcv
  vars: {"MCP_DEVBOX_BRAIN_ROOT":"/brain"}
  approve: true when required by mode
```

## Brain environment variable

The same `coolify_set_env` call first verifies or creates the exact persistent
storage, then lists the application's existing environment variables. A missing key
is created with POST. An existing unique key is updated with PATCH on the same
application `/envs` endpoint, identified by the validated key. Duplicate existing
production keys are rejected before any env write. Coolify preview projections are
ignored when determining production-key uniqueness; they are not deleted or treated
as independent production variables.
Submitted values and response bodies containing them are never returned or audited.

## Deployment gate

Do not deploy before the persistent storage is verified. Create one deployment plan,
execute it once, retain its `deployment_id`, and observe that same deployment until a
terminal state. Do not start a second deployment to repair an observation timeout.

Production closure requires all of:

- the deployed commit is the merged release commit selected after this fix PR;
- application health is green;
- runtime reports 67 tools;
- catalog hash is `sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed`;
- storage remains exactly verified;
- `mcp-catalog-smoke` and `brain-smoke` pass without private output;
- the annotated `p9` tag is created only after those checks.

Do not proceed to frontend work until every closure condition is recorded.
