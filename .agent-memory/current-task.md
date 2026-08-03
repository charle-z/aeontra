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
