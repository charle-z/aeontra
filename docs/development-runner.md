# Isolated development runner

Status: implemented. Enable the administrator-pinned profile explicitly and
validate each registered template and exact repository command as described
below. Unit and package tests do not establish acceptance on another VM or Edge.
Record source, deployment, device and command acceptance separately for each
installation.

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
The template identity also binds the versioned trusted Go probe required by the
broker. An earlier successful calibration cannot attest a changed compiled Go
capability. A valid calibrated template missing a command requirement reports
`awaiting_reasoning` with `new_requirement` without dispatching a workload;
unknown or pending calibration remains pending.

A template attestation describes a provisionable one-shot profile, not a living
VM. Every workload creates a new VM and repeats every kernel, rootless-engine,
filesystem and CI gate before running its exact command. A terminated VM is
never advertised as an available execution lease. Updating the workflow SHA,
generation or provider configuration requires fresh calibration. Settle active
effects before rolling out a changed profile pin or compiled probe contract.
Incompatible captured bindings fail closed; journals are retained and effects
are neither retargeted nor replayed by the update.

The two workload profiles are fixed:

- `make-validate-all`: `make validate-all`.
- `go-test-all`: `go test ./... -count=1`.

The public `project_development_start` admission must contain that exact argv,
an empty cwd/stdin/environment, the explicit registered `runner_profile`, and
`timeout_seconds: 4200`. Other options are rejected before staging. The private
GitHub adapter receives the fixed command-profile identifier, not those bytes.

For `go-test-all` only, an updated Edge can add versioned Go requirement
provenance to the private inspection result. Root `go.mod` and `go.work` `go`
directives are [minimum Go versions](https://go.dev/doc/toolchain#module-and-workspace-configuration),
not exact patch pins. A minimum at or below the provider's pinned Go version
requires that actual exact provider capability; a higher minimum remains
unsatisfied. Go pins from `.tool-versions` and `mise.toml` retain their precision,
and all explicit caller requirements remain required. The provider does not
claim capabilities for older Go patches or unrelated package managers.

The additional evidence requires clean committed source, tracked HEAD manifests,
canonical bounded values and the same source digest before and after reading.
Ignored/untracked Go evidence or older Edge results without this metadata retain
the conservative original requirements. If the optional collector cannot
establish reliable provenance, it omits the metadata and preserves those same
requirements. Malformed or conflicting metadata received by the backend fails
closed. Make, unknown commands and other command options retain their
existing inference. Existing objective contracts are immutable and are not
migrated to the new inference.

An exact Corepack `packageManager` pin with a complete SHA224, SHA256, SHA384
or SHA512 hexadecimal suffix retains its numeric version before bounded
requirement conversion. The generic package-manager requirement and conflicts
are preserved. Unsupported or malformed integrity syntax remains unresolved;
this syntax check does not download or verify the package-manager artifact.

There is no public runner tool, caller argv, shell string, arbitrary environment,
prompt, credential, helper URL or source-body input. The private request binds
source owner, repository, exact public Git SHA, source digest, command profile,
plan digest, stable effect identity, job and fence. Private or unavailable source
stays on Edge. A dirty local tree cannot be transported as this public SHA;
normal governed Git publication and explicit location approval must precede it.

## VM authority and measured capabilities

Before starting rootless Docker, fixed controller setup verifies
`/sys/module/overlay/parameters/redirect_always_follow` and sets it to `N` in
that disposable VM. This makes `redirect_dir=off` resolve to the kernel's
`nofollow` policy instead of implicit redirect following. Unknown values,
missing or non-root-owned parameters, symlinks, and unsuccessful writes block
preparation. No module is reloaded. The Edge and VPS kernel settings are
unchanged. The original overlay mount diagnostics and complete upstream command
remain required evidence; this setup alone does not establish acceptance.

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
The fresh account receives 262144 unused subordinate UID and GID IDs through
`useradd`'s allocator. The default 65536-ID outer mapping cannot contain a nested
test user's range `100000..165535`. Calibration reads the kernel UID/GID maps
inside the pinned container and requires the full allocation and the expected
non-root host owner. This is subordinate-map capacity evidence, not evidence
that every privileged kernel test will pass. Other users' assignments, memory
limits, task limits and controller credential protections are unchanged.
The daemon runs as a systemd user service with its user bus and runtime directory.
Its home is created exclusively as a new empty directory. Before transferring
ownership or creating source/runtime descendants, setup removes only that root's
inherited access/default ACLs. A controller UID inherited from the hosted VM's
`/home` default ACL may be unmapped inside rootless Docker and make volume
population fail. Existing paths, identity drift or unexpected ACL errors block
setup. Parent, application and image ACLs are never scrubbed.
Its fixed `env -i` launch clears the manager's inherited environment and assigns
only workload-owned HOME/XDG paths, the pinned binary path and the user's bus.
An instance-specific, root-owned `user@UID.service` drop-in delegates controllers
and limits the entire manager subtree to 10 GiB and 4096 tasks, including container
scopes outside the daemon unit. Calibration checks the real cgroup driver/version,
ancestor limits and a container's enforced memory/pids values.
The rootless child verifies its non-host UID mapping and copied-up `/run` mount
before unlinking the three fixed inherited runtime symlinks. It never follows
or removes their host targets. Unexpected non-symlink entries block startup.

The untrusted workload receives a user-owned rootless Docker socket, selected
source and its own home. The controller's files/home and process environment are
protected by separate UID ownership, private file modes and cross-UID ptrace checks.
The disposable VM's `hidepid=2` mount exempts only the workload's validated primary
group: runc/systemd need real process cgroup metadata to identify the user bus.
This exposes ordinarily readable PID, status, cgroup and command-line metadata;
controller arguments must remain credential-free. It does not permit environment,
memory, descriptor or private-file reads. Calibration verifies those denials and
ptrace denial against a live controller-owned sentinel, both as the workload user
and mapped namespace root. No Edge or VPS procfs policy is changed.
Source transport fetches
Git objects directly from one constructed public GitHub origin at the exact SHA,
with full non-shallow history, no credentials or client source reconstruction.
The source's own nested images/dependencies are not transformed into provider
policy and are not claimed to be hermetically pinned by this broker.

Command and calibration diagnostics are untrusted data, never receipt evidence.
A command drains its complete output but retains only the last 16 MiB, discarding
the first partial line after truncation. Numeric capture metadata records the
bytes seen, dropped and retained. A prefix-only log cannot establish the final
failure of a long command.
A command also retains its first observed failure and its last panic with the
following active-test context. When distinct contexts exist, each receives up to
8 KiB of complete lines within the same 16 KiB private sideband; a single context
can use 16 KiB. Exact byte lengths frame the two sections, not log-authored markers.
The first observation can be an expected negative fixture and does not establish
the cause of failure. This fixed sideband is replaced on each command. Lines over
64 KiB and incomplete EOF lines are discarded rather than retaining credential
fragments.
A trusted final step reads only fixed controller-owned probe, daemon and command
logs without following links. It emits at most 16 KiB of prefixed JSON, escaping
control characters and redacting every staged CI credential value before output.
For command logs, it scans at most the last 2 MiB of complete lines. It uses the
bounded sideband when present, or that window's failure context for older logs,
within one 4 KiB encoded context allowance. A dual sideband gives the first
observation at most half that allowance, reserving the remainder for the last
panic; legacy single-context sidebands remain readable. A separate 4 KiB encoded
tail is selected from EOF backwards and emitted chronologically; JSON escaping and
prefixes cannot displace the actual final lines. Earlier probe diagnostics leave
this command allowance reserved. Arbitrary older log text remains unavailable;
diagnostics never change the command outcome or attest acceptance.
Probe errors remain visible even when no command was started. Full private logs
are discarded with the VM; diagnostics do not recover source bytes or authorize
a retry.
Before teardown, fixed cgroup-v2 PID and memory counters are captured for the
rootless user manager and the command unit when still present. The latter may
already have been collected; absent counters are reported as unavailable, not
zero. Current counts are not peak measurements. Event counters can demonstrate
a quota hit but their absence cannot identify the cause of a failed fork. These
diagnostics neither increase limits nor change receipt or acceptance decisions.
After an exact `make validate-all` failure, a fixed workload-UID diagnostic can
read numeric ACLs and namespace maps from its Docker storage and a read-only image
subpath. A pinned, bounded holder runs no binaries from the inspected image and
receives no CI credentials or network. The diagnostic cannot alter image ACLs,
change the storage driver, suppress the command failure or attest success.
All emitted fields remain bounded untrusted diagnostics. Known public GitHub
repository/ref/run metadata is not a credential; CI secrets and unknown staged
values remain redacted without corrupting numeric ACL evidence.

Calibration and failed Make executions also collect an optional, fixed overlay
mount diagnostic before stopping the rootless daemon. A trusted static fixture
runs in the existing rootless Docker namespace, with no network or CI credentials,
a read-only root, 256 MiB/64 tasks and a fresh anonymous `/tmp` volume. It compares
`redirect_dir=off` with and without `userxattr`, plus a separate
`redirect_dir=nofollow,userxattr` control. The original two mounts are unchanged.
It preserves syscall errno,
lower-file verification, unmount status, backing filesystem type and read-only
kernel defaults. Manager PID/memory event counters bracket the experiment.
Container creation and cleanup are each bounded to 5 seconds; execution to 30
seconds. Cleanup targets only the newly captured container ID and its anonymous
volume, never an existing container with the diagnostic's name.
A rejected optional mount is compatibility evidence only, not a failed general
capability, accepted command or reason to alter host settings. Preparation,
verification and cleanup failures remain failures. This fixture does not execute
the upstream overlay helper or establish complete BuildKit acceptance.
The additional control distinguishes an explicit redirect policy conflict from
other restrictions; success does not attest the omitted-`userxattr` mount used
by unchanged upstream tests. No kernel parameter is modified by the diagnostic.

Fresh trusted probes require:

- an actual nested user-namespace PID/proc mount;
- subordinate UID/GID mapping and writable delegated cgroup-v2 control files;
- a responding rootless Docker daemon, exact workspace Git bind and nested runc
  execution with a digest-pinned fixture;
- real anonymous `/tmp` volume population and bounded write/read verification,
  with a read-only container root, no network and enforced memory/PID limits;
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

Deploy a backend that understands the additive Go inspection metadata before
installing its signed Edge release. A new backend accepts older Edge evidence
conservatively. Older backends strictly reject unknown result fields, so restore
the older Edge release before rolling back the backend; settle captured effects
and preserve their matching journals before either rollback.
