# Current task — clean post-handoff Edge release

## Verified production base

- PR #132 merged at `842e4e27a9029627edbb0129f7ccd95d718d3360` after 16/16
  exact-head checks and made the managed Front Door preview expose its complete
  dual-catalog transition contract.
- PR #131 merged at `08070734b9827c8efda8d67e922b057f70f7b3d0` after 16/16
  exact-head checks and is deployed. PR #133 synchronized closure documentation at
  `f8c0ce6a25ed46ae4bc4031656b04eb4c3e88603`; production serves that commit with the
  same public catalog.
- Production serves protocol `2024-11-05`, 135 tools and catalog
  `sha256:557d3cbb956a311429dbcf893b329b5b0d0dea5e38a0c8dbae96abac52b1e7dd`.
- PR #135 merged at `6b6c3bcf7c019ebad4568baca8114ad7407d0565` after
  16/16 exact-head checks, and production serves that exact commit.
- Managed Front Door deployment `x5a11ixcdfeo8c3e46ofry38` accepted the previous
  catalog only as one temporary authenticated transition catalog. After the backend
  replacement, deployment `thvv358cgxkzu4qv87z8hfp8` removed it. The final preview
  reports no transition catalog and no pending catalog change.
- Public `/mcp` returns the expected OAuth challenge with the new catalog, backend
  commit and 135-tool identity. Protected-resource and authorization-server discovery
  return valid JSON through Front Door commit
  `489a64f40cbbde014986ff130662a485f9513d6c`.
- Brain note `gpt-web-direct-edge-h5-gh-broker-preflight` retains the deployment
  evidence and authority boundary.

## Verified real-device state

- Official `p15.0.18` is installed on the real Parrot Edge at commit
  `d1ea0781984a531eb70126f256e5326215d90c87`. Doctor reports ready, valid bundle,
  one managed process, held lock, rootless Podman and an empty lifecycle journal;
  the managed unit has `NRestarts=0` and the manual legacy unit remains
  inactive/disabled.
- Real Hito 3A acceptance passed. Hito 3B and Hito 4 exposed the two defects recorded
  in `docs/baselines/2026-08-03-p15-real-edge-acceptance-fixes.md`; their corrective
  candidate is the active task. Git synchronization rejected safely and the normal
  `gh` login was reimported before its next direct retry.
- Official release `p15.0.13` was published from `f8c0ce6a25ed46ae4bc4031656b04eb4c3e88603`
  and installed once on the paired Parrot Edge. Bundle, service, pairing and process
  coherence were verified healthy.
- A human completed normal `gh auth login` as `charle-z`, and
  `mcp-edge github import-gh --owner charle-z` returned only
  `{configured:true,owner:"charle-z"}`. The credential remains in owner-only Edge
  state and was not exposed.
- The private direct broker executes only constructed, bounded `gh api` reads for the
  repository already bound to the selected development project.
- `project_github_status` returns safe repository metadata and closed contents, PR and
  Actions capability probes without accepting token, URL, endpoint, path or raw CLI.

## Confirmed updater history and completed operator handoff

`p15.0.13` still obtains `gh` only through the Debian dependency path. The signed
archive updater does not invoke APT, so a clean archive update cannot guarantee the
broker executable. The correction must not put an unsigned extra file beside a v2
manifest and cannot jump directly to manifest v3 because the installed v2 updater
does not understand it.

PR #134 merged the bridge at `e78436da697db634be4159ce86a7116871bb7c4f`
after 16/16 exact-head checks. Official workflow run `30776878699` published signed
`p15.0.14`. The update installed that release under `/opt/mcp-devbox/current`, but
device inspection found the older enabled `mcp-devbox-edge.service` still running
the `p15.0.13` process outside the managed templated unit. It retained the state lock
while the current unit restarted and failed closed.

PR #135 delivered manifest v3 with pinned official `gh` 2.97.0 and its reviewed
SHA-256. Official run `30778625101` published signed `p15.0.15`. The first real update
correctly failed closed and restored `p15.0.14`: the root updater used
`systemctl disable --now` while the legacy Edge being stopped was also the caller
waiting for that updater. The managed unit never reached a healthy handoff and the
legacy process retained the lock.

PR #136 merged at `78e438972c80f616f18acce079400f2ee034e846` after 16/16
exact-head checks. Official workflow run `30779857409` published signed `p15.0.16`.
The operator then disabled the onboarding path, stopped both processes, proved the
lock free, started the managed templated unit and re-enabled the path. The legacy
`mcp-devbox-edge.service` is a manually installed, non-dpkg unit and remains
`inactive/disabled` as a temporary rollback artifact. Identity, workspaces, GitHub
authority and installed bundle state were preserved.

Real operation `eo_a4753d4f76261c026aad08bd5cba7a41` attempted `p15.0.16`
exactly once after that handoff. Activation still failed closed and restored
`p15.0.14`; status operation `eo_e7985e48fb445715a6c9701f04a112d8` verified the
rollback as valid, active, single-process and managed. The exact signed-unit diff from
`p15.0.14` to `p15.0.16` consists only of `Conflicts` and `After` against the two
legacy names. The clean correction removes those transitional directives. It does not
add privileged pre-start code, stop arbitrary units or delete the operator's legacy
file.

The exceptional runbook now includes the previously missing trigger rule:
`mcp-devbox-edge-onboard@<user>.path` must be disabled during both the forward handoff
and a legacy rollback because `identity.json` always exists and otherwise relaunches
the templated service.

PR #137 merged at `c4cf14669a845935a785e442047571fcfbeab0a0` after 16/16
exact-head checks, and production serves that merge with the unchanged 135-tool
catalog. Official workflow run `30782426563` published signed `p15.0.17`. Real update
operation `eo_8f6258eb03477a442199fc65c1642bc3` attempted it exactly once and
failed closed before unit installation, restoring healthy `p15.0.14`.

The confirmed blocker is the root updater sandbox, not the managed Edge unit. Its
`ProtectSystem=strict` policy omitted `/usr/local/bin` from `ReadWritePaths`. The
installed v2 bridge had never exercised that write because its own bundle contains no
`libexec/gh`; every v3 bundle requires the updater to create the fixed managed
`/usr/local/bin/gh` link before installing the Edge unit. Update and rollback need
that same exact path. Repair already owns it. The correction grants only that fixed
directory to the two closed root lifecycle units; it does not grant root to the Edge.

## Next exact action

Publish the real-acceptance corrections through a normal PR after exact-head gates.
The next signed release must be installed through one normal closed archive update,
which simultaneously proves the corrected updater sandbox and delivers the H3B/H4
fixes. Re-run Hito 3B signal/restart/cleanup, Hito 4 toolbox/service/restart/cleanup and
successful direct Git status/fetch on the real Edge. Do not repeat the already failed
`p15.0.17` operation or use a manual package install for the next release. Hito 9
multiagent/task-graph work remains outside the current authorization.

## 2026-08-03 p15.0.19 acceptance follow-up

PR #139 merged at `52370dceb9bb6d829d8c7ab88e659239677047b8`; official
workflow run `30786403458` published signed `p15.0.19`, and exactly one normal stable
update installed it. Doctor reports ready, one process, held lock, managed coherence,
empty journal and `NRestarts=0`.

The Hito 3B retry is deliberately paused before its single operator restart with one
durable workload still running and incremental output proven non-duplicated. Hito 4
created and started the rootless toolbox container, but the public operation failed
while verifying ownership: Podman returns the container `.Image` as bare 64-hex and
the second comparison did not reuse the canonicalizer added for image inspection.
Direct Git status failed before mutation because local Git calls pass an empty
credential into the runner and `strings.ReplaceAll` with an empty old value corrupts
every output character. Both causes were reproduced without reading a credential.

The active candidate canonicalizes the ownership image identity and bypasses token
replacement only when the credential is empty. Publish it through a normal PR and the
next signed Edge release, update once, then reuse the existing toolbox/process state
to finish H3B/H4 and repeat direct Git status/fetch. Do not restart the Edge before
the toolbox and service are prepared.

## 2026-08-03 p15.0.20 orphan follow-up

PR #140 merged at `4612fd80208717e9749174663c2995f612eaf56f`, official run
`30788597266` published signed `p15.0.20`, and one stable update installed it. Direct
Git status/fetch now passes with a clean synchronized checkout. Hito 4 recovered the
existing toolbox, persisted state and tools, verified the real rootless Podman socket
and started a durable service; its container survived the coordinated Edge restart.

Hito 3B preserved continuous output across the managed update, but its row was marked
`process_identity_changed` and public cleanup removed metadata while the private worker
and Bubblewrap child remained alive. The operator verified and terminated only those
two H3B groups; Edge doctor remained ready and Hito 4 was unaffected. The active final
candidate recovers a revalidated live worker identity before offline classification
and makes cleanup refuse removal while the exact journal, worker or child identity is
still alive. Finish normal gates, PR, signed release and one short real restart/signal
acceptance. Hito 9 remains excluded.

Candidate `f98f635d84080f26f885e8988192ed026423599b` passed the full test suite,
vet, build and diff check from an isolated ext4 clone with Go 1.26.5. The only mounted
NTFS failure was the known `0777` versus `0755` builder-mode mismatch, and that package
passed unchanged on ext4. Publish this exact behavior through the normal PR/CI path.

## 2026-08-03 p15.0.21 sandbox-leader follow-up

PR #141 merged at `7ce6ebd4e35b8c3325395155c30deb4f98c8a99a`; official run
`30810209878` published signed `p15.0.21`, and exactly one stable update installed it.
The private identity repair passed its real restart gate: the fresh H3B process stayed
running and cursor output remained continuous. Closed interrupt and bounded stop still
failed to stop the workload because Bubblewrap's `--new-session` inner leader owned a
different process group from the outer supervisor recorded at `exec.Start`. The exact
inner test group was terminated and exclusively cleaned; no orphan remains, doctor is
ready and `NRestarts=0`.

The active candidate reserves Bubblewrap `--info-fd`, persists only the revalidated
inner leader and targets that exact group. Bubblewrap is now parent-bound to the
durable worker, not to the restartable Edge service, preserving restart recovery while
closing worker-crash orphans. Finish gates, normal PR, next signed release and one
short real H3B restart/interrupt/stop/cleanup acceptance. Hito 9 remains excluded.

Functional commit `2a847b8337bf13cd5e89e751d8c607a0e1959a3e` passed the full
test suite, vet, build and diff check from isolated ext4 with Go 1.26.5. Publish this
behavior by normal PR, then use one next signed release/update and repeat only H3B.

## 2026-08-03 p15.0.22 identity-readiness follow-up

PR #142 merged at `e6dfb77b5bcf74db3760705ec5fe7bec1c5b042c`; official run
`30812656084` published signed `p15.0.22`, and one stable update installed it. The Edge
is ready, single, managed and at zero restarts. A fresh H3B start failed before public
readiness although its bounded probe ran to completion. Bubblewrap reports the inner
child before `--new-session` necessarily completes `setsid`, so the immediate
`PGID == PID` validation raced the host. The active branch adds a two-second bounded
wait that preserves owner and start-time revalidation. Publish it normally, release
the next signed bundle once and repeat only H3B; Hito 9 remains excluded.
