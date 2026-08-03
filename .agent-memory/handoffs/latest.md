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
