# Direct Edge operation lifecycle

Direct GPT Web actions use the existing server-owned `edge_operations` journal. The
same lifecycle now carries bounded foreground execution and durable background process
control; these operations do not start or use a second OpenCode model runtime.

## Durable states

An operation moves through a closed state machine:

- `queued`: persisted and waiting for its paired Edge.
- `leased`: assigned to one Edge process under a signed, expiring lease.
- `succeeded`: completed with a validated bounded result.
- `failed`: completed with a stable safe code and no result body.
- `cancelled`: cancellation won before a terminal success or failure.

Terminal states are immutable. An expired normal lease returns to `queued`. An expired lease with cancellation requested becomes `cancelled` instead of being executed again. Status reads, active-operation listings, cancellation requests and idempotent retries normalize expired leases immediately; they do not wait for the Edge to poll again. Requeued attempts clear prior progress so the new lease can restart progress at revision `1`.

An authenticated completion whose result does not satisfy the bounded schema fails terminally as `operation_result_invalid`. The invalid payload is discarded instead of being persisted, and the lease is not requeued indefinitely. Invalid device, operation or lease identities remain rejected without changing state.

Root-owned bundle update, rollback and repair operations have an additional fail-closed recovery budget. They may recover through at most four leases and for at most twenty minutes from their first pickup. If either boundary is exhausted, the operation becomes `failed` with `operation_recovery_exhausted` instead of relaunching a privileged systemd effect indefinitely. Ordinary diagnostics, project operations and other interruptible work keep the normal requeue behavior.

Legacy rows are migrated transactionally. A bundle operation that was already `leased` receives one recorded attempt and its original creation time as the conservative first-pickup boundary, so an old restart loop cannot survive a server upgrade.

## Progress and restart recovery

While executing an operation, the Edge sends signed progress heartbeats. Progress contains only:

- a strictly increasing revision;
- a closed safe phase name;
- optional completed and total unit counters.

A valid progress heartbeat renews the lease for one bounded heartbeat window. Progress JSON is capped at 1 KiB, counters are bounded, and arbitrary messages are not accepted. The journal and progress survive server restarts because SQLite is the authority.

Operation results are validated by operation kind and capped at 64 KiB before
persistence. Foreground output is bounded in the operation result. Background output is
redacted before it reaches private Edge log files and is returned incrementally through
bounded status calls, so the control-plane journal never becomes an unbounded log store.

## Authoritative direct-operation timing

The private journal records operation creation, current lease, signed `running`, signed
`finalizing` and terminal timestamps. Public lifecycle status derives only bounded
microsecond durations: queue, pickup, Edge work, completion and total. It never returns
absolute lease/phase timestamps, device internals, request bodies or raw results.
Expired normal leases clear current-attempt timing before requeue so a later attempt is
not presented as one continuous execution. Legacy terminal rows without timing remain
readable and simply omit unavailable durations.

`project_exec` additionally measures project/workcell preflight, actual child execution
and local result capture/redaction with the Edge monotonic clock. The terminal
`completion_us` interval covers the remaining transfer and persistence path. This
separation is evidence only: it does not add a model runtime, widen the workcell, change
timeouts or expose command content.

Bundle and onboarding diagnostics include systemd `NRestarts` only when the fixed
`systemctl show` response supplied a valid non-negative counter. The public result has
an explicit `service_restarts_known` bit; an unknown value is never presented as zero.

## Durable managed browser sessions

`project_browser_create`, `project_browser_status`, `project_browser_list`,
`project_browser_run`, `project_browser_artifact_read`, `project_browser_close`, and
`project_browser_cleanup` reuse the signed `edge_operations` lifecycle without starting
another model runtime:

- a logical session is bound to one resolved project workspace and human Edge target;
- the private Edge journal stores exact URLs, cookies, profile identity and artifact
  paths, while public results expose only opaque ids and safe URLs without credentials,
  query or fragment;
- every run launches a fresh fixed Chromium process inside a narrower Bubblewrap
  namespace, then closes it after at most 120 seconds; the logical profile and explicit
  cookie jar survive reconnects and Edge restarts;
- one run accepts at most 32 closed actions and 32 KiB of combined caller text. There is
  no arbitrary JavaScript, executable, browser flag, header, cookie, proxy, extension,
  filesystem path or CDP endpoint input;
- a durable receipt is inserted before page interaction. Completed receipts return the
  saved bounded result without repeating effects; interrupted receipts become
  `indeterminate` and are never automatically retried;
- text capture is redacted and bounded to 16 KiB. JPEG artifacts are limited to 2 MiB,
  persisted with `0600`, and read in exact chunks of at most 24 KiB;
- close preserves the private state, and cleanup removes only exact closed-session
  profiles and artifacts after path and symlink revalidation. There is no chat-based TTL
  or automatic cleanup.

Public and loopback network scopes are separate. Public scope uses a local pinning proxy
and permits only public HTTP(S) destinations on ports 80/443. Loopback scope is pinned to
one exact initial high-port origin and may optionally ignore HTTPS errors for local
self-signed development endpoints.

## Durable background processes

`project_process_start`, `project_process_status`, `project_process_stop`,
`project_process_signal`, `project_process_list`, and `project_process_cleanup` extend the
same trusted-workcell executor as `project_exec`:

- start accepts an argv array, relative workspace cwd, optional stdin and a non-secret
  environment overlay; it never adds an implicit shell;
- a caller idempotency key maps the same request to one opaque `pr_...` identity, while
  reuse with different parameters fails closed;
- private SQLite metadata and separate `0600` stdout/stderr files survive chat and
  control-plane reconnects;
- public results expose state, safe timestamps, bounded redacted output, offsets,
  truncation, exit status and terminal signal, but never PID, process group, host path,
  argv, environment or secret material;
- stop revalidates Linux PID start time and process-group identity before sending TERM,
  waits the requested bounded grace period, and sends KILL only if the owned group did
  not stop;
- terminal stop is idempotent and process rows have no TTL or automatic cleanup.
- each background Bubblewrap instance is owned by a minimal per-process worker from the
  same signed `mcp-edge` binary. The worker keeps redaction and wait/receipt handling
  alive independently of the control loop; the Edge systemd unit stops only its main
  process, so a signed restart or update does not implicitly stop managed groups;
- the worker owns Bubblewrap's reserved `--info-fd`, revalidates the reported sandbox
  child PID, stable start ticks, process group and owner before readiness. Bubblewrap
  may publish the child before `--new-session` has completed `setsid`, so the worker
  waits a short fixed interval for `PGID == PID` while rejecting disappearance, PID
  reuse or foreign ownership. It then persists that
  inner session leader rather than the short-lived outer Bubblewrap supervisor. Closed
  signals target only that exact group. Bubblewrap is parent-bound to the durable
  worker, so an Edge restart remains transparent while a worker crash cannot orphan
  its sandbox;
- manager startup and every bounded list/cleanup recovery pass reconcile active rows
  against PID, Linux start ticks, process group and owner identity; reused, foreign,
  incomplete and disappeared identities become safe terminal states instead of being
  signalled;
- list returns at most 100 opaque lifecycle summaries and signal accepts only the
  closed `interrupt`, `terminate`, or `kill` enum;
- cleanup is explicit, individual or project-scoped, idempotent, and removes only
  terminal metadata and private logs. Journal, worker and sandbox identities are all
  revalidated before removal; any exact live identity is counted and preserved.

Emergency concurrency and per-stream storage ceilings are administrator-controlled
Edge flags. They are protection against host exhaustion, not per-command allowlists or
process expiration.

## Safe checkout synchronization

The registered development checkout has a separate closed Git synchronization path:

- `project_git_status` reads the attached branch, HEAD, fixed upstream, live remote
  HEAD, fetched relation and clean/dirty/diverged state without returning paths or URLs;
- `project_git_fetch` runs `git fetch --no-tags origin` with only the Edge-constructed
  current-branch tracking refspec through the existing owner-bound private credential
  runner; callers cannot supply URLs, tags or refspecs;
- `project_git_fast_forward_preview` requires a clean attached branch, current
  remote-tracking state, zero local-only commits and an ancestor relationship, then
  persists a private `0600` five-minute plan bound to the exact two commits;
- `project_git_fast_forward` consumes that plan once, revalidates every bound field and
  runs only `git merge --ff-only` of the recorded remote commit.

Fetch does not touch the working tree. Fast-forward rejects dirty, detached, ahead,
diverged, owner-mismatched, stale-remote, expired, replayed, malformed or symlinked
state. There is no `reset --hard`, checkout mutation, force, tag fetch, arbitrary
remote, arbitrary refspec or credential-bearing output.
Only `ls-remote` and fetch receive the private askpass credential. Local inspection,
ancestor checks and `merge --ff-only` run with an empty credential environment, so
repository-controlled filters cannot inherit the GitHub token.

## Private GitHub authority preflight

The first Hito 5 direct GitHub operation reuses the registered project identity without
starting OpenCode or another model runtime:

- `project_github_status` accepts only project alias and human Edge target;
- the Edge requires the project to remain a ready `linux-workcell`/`dev` registration
  whose owner matches the private credential;
- the broker invokes the official `gh` binary only with server-constructed repository
  metadata, bounded pull-request and bounded Actions `api` reads;
- `GH_TOKEN` is present only in that child environment under a private HOME/XDG root;
- the control plane receives only repository identity, visibility, default branch,
  archived state and closed metadata/contents/PR/Actions/administration booleans.

There is no caller-selected repository, endpoint, URL, header, token, GraphQL body,
pagination, raw CLI result or arbitrary `gh` command. Consequential PR, workflow and
release operations remain separate Hito 5 contracts and are not implied by this
read-only preflight.

## Persistent rootless toolbox

The registered development workspace can own one persistent toolbox independently of
OpenCode or a model runtime:

- `project_toolbox_create` pulls the server-owned Debian base through the Edge user's
  validated rootless Podman/Docker endpoint, records its exact image ID, creates a
  labelled container with the selected workspace at `/workspace` plus the already
  validated user-owned rootless engine socket at a fixed internal path, and starts it
  with an idle process;
- creation accepts optional CPU millicores, memory MiB and process-count caps. Missing
  values receive server-owned defaults of 4 CPUs, 8 GiB and 2048 processes; accepted
  ranges remain broad enough for builds while rejecting zero, negative or excessive
  caller values;
- private metadata is one owner-only `0600` record under Edge state. The opaque
  `tb_...` identity, base image identity and timestamps survive chat, backend, Edge and
  WSL restarts as long as the user-owned container storage survives;
- `project_toolbox_exec` and `project_toolbox_install` accept explicit arbitrary argv,
  optional relative cwd and a non-secret environment overlay. They add no shell and no
  command, language, package-manager or destination allowlist;
- execution runs as container root inside the rootless user namespace, so `apt`, npm,
  pip, Go modules, Cargo and other toolchains can persist without changing the host WSL
  package database;
- public results are bounded and redacted and expose no host path, socket, container
  name, raw engine identifier or environment. The applied CPU, memory and process caps
  plus rootless-engine availability and writable/rootfs byte usage are safe public
  metadata;
- every status, execution, service and cleanup path revalidates the actual rootless
  container's memory bytes, nano-CPU quota and PID cap against the private record.
  Reusing a toolbox with different requested limits fails closed rather than mutating
  a long-lived environment implicitly;
- the fixed toolbox environment owns `DOCKER_HOST`, `CONTAINER_HOST`, engine kind, a
  parent label and a deterministic Compose project name. Caller environment cannot
  override them. An installed remote client can therefore perform rootless Podman,
  Docker, Compose and engine-native builds without receiving the host socket path;
- ownership requires exactly two writable binds: the selected workspace and the
  current validated rootless socket. Any extra mount, rootful endpoint, socket drift,
  reserved-environment drift or resource drift rejects the toolbox;
- `project_toolbox_repair` revalidates the recorded image, labels and exact single
  workspace mount before restarting a stopped or created container. It does not create
  a replacement when private state, ownership or container identity is missing or
  unsafe;
- `project_toolbox_service_start/status/stop` manage background argv in that same
  toolbox. Service names are closed, public identities are opaque `ts_...` values, and
  private PID plus `/proc` start ticks prevent PID-reuse confusion. Status is read-only
  and reports stopped when the container is stopped; stop uses TERM, bounded grace and
  KILL only when necessary;
- the Edge does not persist service argv or environment. A service survives chat and
  Edge-daemon restarts while its rootless container keeps running; after the container
  or WSL itself stops, its durable identity reports `stopped` and a fresh explicit
  start creates a new opaque service identity rather than silently replaying old argv;
- `project_toolbox_cleanup` is the only automatic product path that removes the
  toolbox, and it runs only when explicitly called. It removes neither the project
  workspace nor unrelated rootless resources.

The toolbox deliberately uses a server-owned Debian base and one container per
workspace. Foreground execution, installation, repair and background service lifecycle
all reuse that identity and storage rather than creating a second toolbox family.

## Cancellation

`edge_operation_cancel` is idempotent for an already cancelled operation.

- Any queued operation can become `cancelled` before pickup.
- An interruptible leased operation records `cancel_requested=true`.
- The Edge observes that flag in the signed progress response, cancels its operation context and acknowledges the cancelled terminal state.
- A terminal completion update requires `cancel_requested=0`, so a late success cannot overwrite an accepted cancellation.
- Signed bundle update, rollback and repair operations are not interruptible after pickup because stopping the control operation alone would not reliably reverse the root-owned systemd effect.

If the Edge disappears after an accepted cancellation request, lease expiry closes the operation as `cancelled` during the next queue recovery pass.

## Recovery from another chat

`edge_operation_list` accepts a human Edge target alias and returns only active queued or leased operations. `edge_operation_status` reads one operation by its opaque operation id. Their output is limited to:

- operation id, kind and state;
- cancellation flag and whether cancellation is currently allowed;
- bounded progress;
- safe project alias and target when present;
- safe terminal reason;
- creation and update timestamps.

Device ids, workspace ids, local paths, request bodies, idempotency keys, credentials, raw output and result payloads are never returned by these lifecycle tools.

## Deliberate boundary

This layer provides operation identity, queueing, cancellation, bounded result
transport and direct foreground/background execution with restart reconciliation.
It does not expose PID, host paths, arbitrary signals or a host-wide process list.
An explicit process stop is distinct from cancelling the short-lived Edge operation
which created or inspected it.
- Lifecycle responses expose `cancellable` so callers do not have to infer authority from the operation kind or state.
- `edge_operation_cancel` rejects a leased non-interruptible operation instead of pretending that the effect stopped.
