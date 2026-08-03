# Handoff — operator handoff complete; clean signed Edge release active

Production backend commit `f8c0ce6a25ed46ae4bc4031656b04eb4c3e88603` serves
protocol `2024-11-05`, 135 tools and catalog
`sha256:557d3cbb956a311429dbcf893b329b5b0d0dea5e38a0c8dbae96abac52b1e7dd`.
PR #131 and its integration head passed 16/16 exact-head checks before merge.

The official `gh` package dependency, normal full-account `gh auth login`, private
`mcp-edge github import-gh`, fixed bounded `gh api` broker and read-only
`project_github_status` operation are in source and backend production. Credentials
remain outside the model workcell and only bounded parsed metadata reaches MCP.

Front Door commit `489a64f40cbbde014986ff130662a485f9513d6c` stayed healthy
through the managed two-deployment transition. Deployment
`x5a11ixcdfeo8c3e46ofry38` temporarily accepted both authenticated catalogs while the
backend changed; deployment `thvv358cgxkzu4qv87z8hfp8` then retired the previous
134-tool catalog. A final managed preview reports an empty transition catalog and no
pending catalog mutation. Public MCP challenge and OAuth discovery return the new
identity.

Official release `p15.0.13` was published from the production commit and installed
once on the paired Parrot Edge. Bundle and service are healthy. The human login as
`charle-z` and `mcp-edge github import-gh --owner charle-z` completed without exposing
the credential.

PR #134 merged the v2 bridge at `e78436da697db634be4159ce86a7116871bb7c4f`
after 16/16 checks, and official run `30776878699` published `p15.0.14`. Its files and
current link installed successfully. A previously enabled legacy
`mcp-devbox-edge.service` kept the old `p15.0.13` process and state lock, so the
templated managed unit failed closed instead of creating a duplicate.

PR #135 merged at `6b6c3bcf7c019ebad4568baca8114ad7407d0565` after
16/16 checks and official run `30778625101` published signed `p15.0.15` with pinned
official `gh` 2.97.0. Real operation `eo_a5b534df08797bd152d018d5fc1a65be`
failed closed and restored `p15.0.14`. The updater attempted `disable --now` against
the legacy service whose Edge process had launched and was awaiting that updater, so
the managed unit never completed the handoff and the old process retained the lock.

PR #136 merged at `78e438972c80f616f18acce079400f2ee034e846` after 16/16
checks, and official run `30779857409` published signed `p15.0.16`. Before its real
update, the operator disabled the onboarding path, stopped both fixed Edge services,
proved no `mcp-edge` process remained, started the templated service, verified it held
the single state lock and re-enabled the path. The legacy unit is an unpackaged manual
artifact and remains inactive/disabled for rollback; no identity, workspace, GitHub
authority or bundle state was lost.

Real operation `eo_a4753d4f76261c026aad08bd5cba7a41` still failed closed and
restored `p15.0.14`. Follow-up status
`eo_e7985e48fb445715a6c9701f04a112d8` verified the rollback healthy. The only signed
unit delta between `p15.0.14` and `p15.0.16` is the transitional `Conflicts`/`After`
pair. The current branch removes only those directives and adds a regression that
forbids them. It also records that `mcp-devbox-edge-onboard@<user>.path` must stay
disabled throughout a forward handoff or a rollback, because the existing identity
otherwise relaunches the templated service.

PR #137 merged at `c4cf14669a845935a785e442047571fcfbeab0a0` after 16/16
checks; production serves that exact merge. Official run `30782426563` published
signed `p15.0.17`. Real operation `eo_8f6258eb03477a442199fc65c1642bc3`
attempted it once and failed before new-unit installation, then restored healthy
`p15.0.14`.

The root cause is now exact: the fixed updater service has `ProtectSystem=strict` but
did not include `/usr/local/bin` in `ReadWritePaths`. The v2 bridge contains no bundled
`gh`, so this was latent; a v3 activation must create the fixed managed
`/usr/local/bin/gh` link before installing the Edge unit. The update and rollback
services now receive only that exact additional write directory, matching the existing
repair service. A signed Debian package must be installed once to replace those root
unit files; an archive cannot change the sandbox of the updater already executing.

Finish gates, merge, publish `p15.0.18`, install its signed Debian package once and
require one managed current process plus the live GitHub preflight. Do not add a
privileged pre-start bridge, delete the manual legacy unit, retry `p15.0.17` or run an
OpenCode latency benchmark. Hito 9 multiagent/task-graph work remains deferred.

## 2026-08-03 continuation

Official `p15.0.18` is installed and stable on the real Parrot Edge. The managed unit
is active with one process, held lock, managed coherence and `NRestarts=0`; the manual
legacy unit remains inactive/disabled. Hito 3A passed. Hito 3B exposed that the durable
worker could consume a kill request while its separate Bubblewrap workload group kept
running. Hito 4 exposed Podman 5.4's bare 64-hex image identity. The candidate branch
records and revalidates the private workload-group identity for direct signaling and
canonicalizes both valid Docker and Podman SHA-256 forms. Full tests, vet and build pass
from ext4 with exact Git modes. Publish by normal PR, then use the next signed release
to prove one ordinary archive update plus real H3B/H4/Git acceptance. The complete
operation ledger is in Brain note
`p15-0-18-parrot-trusted-linux-real-acceptance-2026-08-03`.

## 2026-08-03 p15.0.19 acceptance follow-up

PR #139 merged at `52370dceb9bb6d829d8c7ab88e659239677047b8`, official run
`30786403458` published signed `p15.0.19`, and one stable archive update installed it
successfully. Doctor is ready with one managed process, held lock, empty journal and
zero restarts. A new Hito 3B process remains active with cursor-based output proven
non-duplicated; do not restart it until Hito 4 preparation succeeds.

The real Hito 4 retry proved container creation actually succeeds. Its final ownership
check rejected Podman's second bare `.Image` identity against the canonical prefixed
record. Direct Git failed because the isolated runner called `strings.ReplaceAll`
with the empty credential used by local commands, corrupting otherwise valid Git
output. The active follow-up has RED/GREEN regressions for both boundaries. Finish
gates and normal PR, publish the next signed release, update once, prepare the existing
toolbox/service, then perform the single coordinated restart and final cleanup.

## 2026-08-03 p15.0.20 orphan-reconciliation follow-up

PR #140 merged at `4612fd80208717e9749174663c2995f612eaf56f`, official run
`30788597266` published signed `p15.0.20`, and one stable update installed it. Direct
Git status/fetch passed and the Hito 4 toolbox plus its durable service survived the
single coordinated Edge restart. Final H4 service/marker validation and scoped cleanup
remain with the caller that owns the direct tools.

Hito 3B did preserve continuous output across the restart, but the server classified
the process as `process_identity_changed` and accepted cleanup while the old private
worker and Bubblewrap child were still live. The exact orphan workload groups were
terminated on the host without touching the Edge or H4 toolbox. The active candidate
adds RED/GREEN coverage that recovers a live owner-only worker identity before offline
classification and refuses terminal cleanup while either exact private identity is
alive. Finish the full gates and normal PR, publish the next signed release, update the
Edge once, and repeat Hito 3B from a fresh process. Do not accept H3B from
`p15.0.20` or from the removed `pr_33ae97c1002ecadb66b9366b3a2c69a7` workload.

## 2026-08-03 p15.0.21 sandbox-leader follow-up

PR #141, signed `p15.0.21`, production deployment and one official Edge update are
complete at `7ce6ebd4e35b8c3325395155c30deb4f98c8a99a`. Git and Hito 4 are
accepted and H4 was exclusively cleaned. The new H3B process recovered across one Edge
restart with continuous output, proving the prior reconciliation fix, but interrupt
targeted Bubblewrap's outer supervisor while its `--new-session` inner process group
kept running. Bounded stop failed. The operator terminated only the revalidated inner
test group, then repeated stop and exclusive cleanup succeeded; doctor is ready with
zero restarts and no matching workload.

The active branch must finish the `--info-fd` correction: persist the exact reported
inner leader after owner/start-ticks/PGID validation, signal that group, and bind
Bubblewrap to the independent durable worker with `--die-with-parent`. Publish through
normal PR and the next signed release, then repeat only the short H3B real acceptance.

## 2026-08-03 p15.0.22 identity-readiness follow-up

PR #142, signed `p15.0.22`, production deployment and exactly one Edge update are
complete at `e6dfb77b5bcf74db3760705ec5fe7bec1c5b042c`. Doctor is ready and
`NRestarts=0`. The first new H3B start failed before returning an opaque process because
Bubblewrap published `child-pid` just before `--new-session` completed `setsid`; the
immediate `PGID == PID` check lost that race. The bounded harmless probe ended and no
matching workload remains. The current candidate waits at most two seconds for the
same owned, unchanged-start-time child to become group leader. Publish by normal PR,
release/update once, then repeat only H3B restart, signal, idempotent stop and cleanup.

## 2026-08-03 p15.0.23 accepted handoff

PR #143, signed release `p15.0.23`, production commit
`b235d2040aa2c62e2b4a134fbf0e2763baf1c246` and exactly one official Edge update are
complete. H3B is accepted on the real Parrot Edge: continuity survived one restart,
public interrupt stopped the actual inner workload with known code 130, repeated
signal/stop were idempotent, cleanup was exclusive, the final list was empty, no marker
remained and doctor was ready with zero restarts. Brain evidence is
`p15-0-23-parrot-trusted-linux-h3b-acceptance-2026-08-03`. H4 and Git remain accepted;
do not repeat these gates unless behavior changes. Hito 9 was not started.
