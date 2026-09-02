# ADR 0006 — Governed development environment

Status: **Accepted for implementation**

Date: 2026-09-02

## Context

The private L3 runner reported itself available while requests as small as `pwd`
failed with an undifferentiated HTTP 400. Production configured the runner with a
multi-repository root. The executor scanned and mounted that complete root for every
request, so a policy-denied path in one sibling repository rejected work in every
repository. A successful request would also have exposed sibling checkouts inside the
container.

The durable Edge queues already have idempotency, leases, fences, heartbeat and crash
recovery. Their oldest-first selection can nevertheless let a repeatedly expired item
return to the front of the queue. Toolchain support is split between a fixed,
networkless L3 image and persistent Edge toolboxes, but that split was not reported
clearly enough for a project to choose the correct surface.

## Decision

Development authority is divided by effect, not by command name:

```text
MCP client
   |
   +-- repository reads and patch-first writes
   |
   +-- private L3 runner
   |     one selected repository
   |     arbitrary non-privileged argv
   |     local Git mutation
   |     fixed core toolchains
   |     network denied
   |
   +-- paired Edge project
         trusted host-shared workcell or persistent rootless toolbox
         repository-selected network policy
         alternate or installed toolchains
         durable processes and browser harness

Remote publication
   preview -> exact revalidation -> single-use execution
```

### Workspace selection

The public process resolves the requested `cwd` inside its configured jail. The wire
request contains only an opaque workspace identity, an optional direct-child scope and
a relative directory. It never contains a host path.

When the configured runner root is one Git repository, the complete relative directory
is retained and no scope is accepted. When it is a multi-repository root, the request
must select one direct, non-symlink directory. The runner scans and mounts only that
directory. It never falls back to mounting the parent root.

### Execution and Git

`sandbox_exec` accepts arbitrary explicit argv inside L3 when the administrator has
selected `allow` mode. Local Git operations, including branch, index, commit, merge,
rebase and stash operations, use that contained execution boundary and do not require
a privileged task profile. Network publication remains impossible in the networkless
container and continues through the existing planned publication tools. Force push and
caller-selected remotes remain forbidden.

`run_command` keeps its smaller executable allowlist for compatibility. It uses the
same selected repository and private runner; a policy denial is distinct from an
executor or workspace failure.

### Readiness and errors

The runner advertises profile `l3-v2` only after an actual ephemeral container proves:

- the pinned image and rootless engine identity;
- network profile `none`;
- writable `/workspace` and executable argv;
- a private executable `/tmp` for compiler and test outputs;
- Git and the fixed core toolchains.

The probe uses a private disposable directory, not a repository root. Readiness may be
cached briefly, but every request still revalidates and scans its selected repository.
The client accepts `available=true` only for the exact profile, image digest, backend
and network posture.

Errors crossing the private HTTP boundary use bounded stable codes. Invalid input,
workspace selection, working directory, secret preflight, receipt conflict, timeout,
engine unavailability and internal execution failure remain distinguishable. Error
responses never include host paths, engine responses or secrets.

### Queue recovery

An expired lease loses its original queue position before retry. A bounded retry
budget converts repeatedly abandoned work into a terminal, diagnostic failure rather
than allowing it to monopolize the Edge. Queue limits count nonterminal work, so
retained terminal evidence does not prevent new admission. Fair selection applies
between workspaces in the generic scheduler; device and project identities remain
bound to their existing server-owned records.

### Toolchains

L3 remains a small immutable core for Git, shell, Go, Rust/Cargo, Python, Node/npm and
C/C++. It does not download dependencies at runtime and does not claim Java, pnpm or
arbitrary repository-selected versions.

The Edge project toolbox is the provisioning surface for Java, pnpm, alternate
versions and project-specific dependencies. Its root filesystem and caches persist
until explicit cleanup. Manifest inspection is read-only and advisory: repository
files can declare requirements but cannot broaden network, mount, credential or host
authority. Install commands remain explicit and auditable.

## Compatibility and rollout

`profile_version` binds each execution to its request shape, and `workspace_scope` is a protocol addition. Legacy runners reject those unknown fields.
Rollout therefore proceeds in this order:

1. deploy the backward-compatible private runner that advertises `l3-v2`;
2. verify its real health probe while the old public backend remains active;
3. deploy the public backend that requires `l3-v2` and sends scoped requests;
4. run selected-repository, Git, error and isolation acceptance;
5. roll back the public backend first if acceptance fails.

An old runner never receives a new request: the new client treats every profile other
than `l3-v2` as unavailable. The new runner accepts the legacy relative-directory form
only when it can resolve it unambiguously beneath a multi-repository root.

## Rejected alternatives

- Mounting the complete multi-repository root exposes unrelated checkouts and keeps
  sibling failures coupled.
- Falling back to host execution removes the containment boundary.
- Adding a privileged profile for local Git confuses repository mutation with remote
  publication.
- Shipping every language manager in L3 makes the image large and still cannot satisfy
  arbitrary project version pins.
- Clearing queues or extending timeouts hides abandoned work instead of recovering it.

## Consequences

L3 becomes useful for ordinary local engineering while retaining its narrow host and
network boundary. Projects needing network downloads or toolchains outside the fixed
core use an authorized Edge toolbox. The rollout has an explicit temporary state in
which L3 is unavailable but fails closed; repository tools, Edge operations, OAuth and
the Front Door remain independent.
