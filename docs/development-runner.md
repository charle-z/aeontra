# Isolated development runner

Status: validation pending. The private broker and workflow are implemented;
unit tests are not evidence that a GitHub-hosted VM completed calibration or a
repository's complete workflow. Source, deployment and Edge acceptance remain
separate facts.

This runner owns one disposable GitHub-hosted Ubuntu 24.04 VM per execution. It
uses the existing source broker's GitHub credential, the existing workqueue
database and the existing reconciliation loop. It introduces no service, second
scheduler, database, paid runner or budget change. Standard hosted runners on
public repositories are covered by GitHub's [public-repository runner policy](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).
Private repositories, single-CPU container runners and larger paid labels are
not supported by this provider.

Configuration ownership remains in [configuration.md](configuration.md); trust
boundaries remain in [security.md](security.md). This document owns this runner's
operation and recovery, not the complete configuration inventory.

## Explicit opt-in and calibration

The administrator registers `github-hosted-ubuntu24-v1`, the configured owner's
public runner repository, one workflow branch, its exact 40-character commit
SHA and a positive template generation. There is no automatic provider selection
or data-location change. The workflow branch is resolved before dispatch and the
run's exact workflow head is checked afterward. This initial adapter supports
branches, not tags. A changed branch fails closed against its SHA pin.

The existing registered Edge `linux-workcell` authority is distinct from the
local L1 command policy; development commands retain the same scoped argv
posture as `project_process_start`. MCP behavior annotations are client hints,
not authorization enforcement. The external VM route additionally requires the
disabled-by-default administrator configuration with exact pins, explicit
request profile and fixed command admission described here.

Before planning any workload on this template, complete a separate registered
`probe-only` calibration against the exact public Aeontra template commit. This
profile fetches source objects but never runs repository Makefiles or tests.
With no configured calibration reference, the first explicitly requested runner
profile calls `EnsureCalibration`: it derives one stable template effect and
job in the existing workqueue, then reconciles that probe without dispatching a
workload. Concurrent requests share that calibration. An explicit administrator
reference is used as-is and is never replaced by a new automatic run.
Cancelling a workload request does not cancel this shared administrator probe;
that request can become cancelled without waiting for calibration. The probe
cannot execute repository commands, and its VM remains bounded by the workflow
timeout.
`TemplateAttestation` accepts only a succeeded `probe-only` receipt belonging to
the exact template identity/generation. Queued jobs, environment variables and
the `runs-on` label cannot supply that attestation.

A template attestation describes a provisionable one-shot profile, not a living
VM. Every workload creates a new VM and repeats every kernel, rootless-engine,
filesystem and CI gate before running its exact command. A terminated VM is
never advertised as an available execution lease. Updating the workflow SHA,
generation or provider configuration requires fresh calibration.

The two workload profiles are fixed:

- `make-validate-all`: `make validate-all`.
- `go-test-all`: `go test ./... -count=1`.

The public `project_development_start` admission must contain that exact argv,
an empty cwd/stdin/environment, the explicit registered `runner_profile`, and
`timeout_seconds: 4200`. Other options are rejected before staging. The private
GitHub adapter receives the fixed command-profile identifier, not those bytes.

There is no public runner tool, caller argv, shell string, arbitrary environment,
prompt, credential, helper URL or source-body input. The private request binds
source owner, repository, exact public Git SHA, source digest, command profile,
plan digest, stable effect identity, job and fence. Private or unavailable source
stays on Edge. A dirty local tree cannot be transported as this public SHA;
normal governed Git publication and explicit location approval must precede it.

## VM authority and measured capabilities

The workflow has `contents: read` only, no production environment, secrets or
OIDC grant. Go, Docker, Buildx, RootlessKit and runner-owned fixture images use
fixed versions/content pins; actions use full commit SHAs. The hosted kernel,
base-image utilities and Ubuntu package repository remain GitHub/Ubuntu-managed
inputs rather than immutable VM-image pins. Go auto-toolchain selection is
disabled. These limitations are not hidden behind the workflow SHA.

Fixed privileged setup runs only inside the disposable VM. It creates one
separate workload UID without sudo or controller group membership, subordinate
UID/GID ranges and delegated cgroup-v2 units. It disables the VM's rootful Docker
service and applies the reviewed unprivileged-userns AppArmor/sysctl change in
that VM only. It does not change an Edge or production VPS posture.

The untrusted workload receives a user-owned rootless Docker socket, selected
source and its own home. The controller's files/home and process environment are
protected by separate ownership and `/proc` hidepid. Source transport fetches
Git objects directly from one constructed public GitHub origin at the exact SHA,
with full non-shallow history, no credentials or client source reconstruction.
The source's own nested images/dependencies are not transformed into provider
policy and are not claimed to be hermetically pinned by this broker.

Command diagnostics are untrusted data, never receipt evidence. A trusted final
step emits at most a 16-KiB log tail as prefixed JSON strings, escaping control
characters and redacting every staged CI credential value before output. The
full private log is discarded with the VM; diagnostics do not recover source
bytes or authorize a retry.

Fresh trusted probes require:

- an actual nested user-namespace PID/proc mount;
- subordinate UID/GID mapping and writable delegated cgroup-v2 control files;
- a responding rootless Docker daemon, exact workspace Git bind and nested runc
  execution with a digest-pinned fixture;
- actual GitHub cache export followed by import into a new, empty pinned
  BuildKit builder under an effect-specific cache scope;
- the fixed Go version, Make and complete non-shallow Git objects.

GitHub [runtime/cache contracts](https://docs.docker.com/build/ci/github-actions/cache/)
require ephemeral Actions runtime authority. A trusted local action stages only
that job's cache/runtime fields in controller-private storage. The workload gets
those ephemeral CI fields, never the source broker token or production secrets.
Cache scopes are unique per effect; this first provider does not restore shared
cross-objective caches. The runtime authority may also permit CI artifact
operations, so repository-authored artifacts are explicitly untrusted.

Kernel or CI failure blocks command execution. A minimal probe is not proof of
every later container workload: the exact repository workflow remains the
acceptance gate. Containerd and stargz capabilities are not inferred from Docker
availability. BuildKit's upstream test image builds its own such dependencies;
that does not attest pre-existing host services.

## Receipts and isolation limits

Repository output is redirected to controller-owned private logs, not emitted
into GitHub's workflow-command channel. Workload processes cannot write
`GITHUB_OUTPUT`, the private command launcher, provider records or a trusted
receipt. Every child runs in a fixed UID/cgroup with memory, PID and time bounds.
The source command is never executed as root.

The broker uses bounded authenticated GitHub API metadata, not a repository log
or JSON artifact, to verify workflow ID/path, exact workflow SHA, run ID,
`workflow_dispatch`, exact effect/plan/execution-digest run-name, first run attempt and all
trusted gate outcomes. Re-runs, changed heads, ambiguous run-name matches,
missing/skipped gates and unverified success produce no receipt. The receipt
binds the source/plan/template/command/job identity and verified run. It is a
server-owned integrity digest, not a cryptographic GitHub artifact attestation
or independent proof of guest-kernel correctness. Semantic objective acceptance
remains separate from exit zero.

Before any execution gate, the pinned workflow recomputes the execution digest
from its actual effect, plan, source owner/repository/SHA, command profile,
workflow SHA and workflow ID. It uses domain-separated SHA-256 with
length-delimited UTF-8 fields. The broker computes the same digest independently
for its expected request and requires the exact resulting run-name; a caller's
asserted digest cannot substitute another source or a `probe-only` profile.

This is a network-enabled disposable VM, not the networkless Edge sandbox.
Rootless Docker is broad authority inside the workload's user namespace.
`--privileged` containers remain limited by that namespace, and may still fail
host-specific tests. The provider does not promise universal egress isolation,
container escape resistance against a compromised kernel, or reproducible
upstream network dependencies. It never installs QEMU or replaces a failing
documented gate with weaker tests.

## Lifecycle, recovery and cancellation

`development_runner_effects` is bounded to 1024 metadata rows in the existing
workqueue SQLite store and is included in its backup. It stores no raw argv,
tokens, source body or log. Its records use revision CAS, immutable dispatch
binding and current authoritative job fences. Startup integrity rejects corrupt
or oversized records. Settled effects older than thirty days may be pruned only
after their queue job is terminal; active/unknown intents and the currently
configured or derived calibration are preserved.

Before the sole dispatch POST, the broker persists `dispatch_intent`. A 204
acknowledgement may contain no run ID; a network failure may occur after GitHub
accepted the effect. Both cases are reconciled by exact effect/plan/execution-digest run-name,
workflow ID, event and head SHA through at most three pages of 100 bounded runs.
No matching run leaves `reconciliation_required`; it never proves non-dispatch
and never authorizes a blind second POST. A process restart reads the same
durable intent. A new effect requires a new governed attempt.

Each broker round has a 15-second ceiling. Long jobs remain pending in GitHub
and are observed in the existing loop. Effects have independent striped locks,
so one remote lookup does not serialize every objective. Cancel persists `cancel_intent` before a
fixed exact-run cancellation POST. A 202/409 is only acknowledgement: the next
read must verify the terminal run. Cancellation never creates a success receipt.
Keep the captured lease heartbeated until the external cancellation reconciles;
an expired/unowned lease cannot create or retarget an effect.

If lookup exhausts its window, identities differ or acknowledgement remains
unknown, retain the journal and report reconciliation rather than deleting it
or replaying the command. Inspect only the captured run through the source
broker. Disabling the provider does not remove source or historical records.

## Acceptance and rollback

Run focused broker/journal/workflow contracts, then the complete source gates.
After normal reviewed publication, validate a real `probe-only` run on the exact
configured workflow head, either through an explicit-profile request with the
automatic probe or an administrator-registered receipt. Only after verified
calibration may the repository's documented complete command run on its exact
public source SHA.
Record the real VM/kernel/CI limits and command outcome as separate evidence.
No runtime acceptance is claimed from source compilation or workflow dispatch.

For rollback, stop admission of new runner requests, cancel/reconcile captured
nonterminal runs, retain workqueue backups and disable the profile. Restore a
previous reviewed template only with an explicit pin and calibration. Neither
rollback nor cleanup deletes the Edge workspace or reconstructs Git history.
