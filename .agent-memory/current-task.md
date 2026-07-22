# Current task — P16 Step 0 specification

Active branch: `p16-global-work-scheduler`
Branch base: `origin/main` at `cc759e391a1c0b3b4410652267ee374514243ee2`
Status: Step 0 contract drafted; no runtime, Coolify, production, or real Edge behavior changed by P16.

## Historical deployed anchors

Later work remains additive and must preserve existing closure evidence:

- P8.1 Console 2.0 is closed and deployed at
  `d343264bffdc0ae1bc045a9d723e913be977090c`, tagged `p8.1`, with its historical
  67-tool milestone and `not_paired` Edge state preserved as closure evidence.
- P9 Brain is deployed as the successor to P8.1.
- P13 opaque continuation, P14 authorized HTB actions, and P15 signed Edge are later
  deployed successors.
- The latest repository-recorded real-host Parrot installation proof remains a
  separate evidence boundary; do not infer a device update, credential state, or
  private-repository smoke from source documentation alone.

## P16 scope frozen in Step 0

- Easy signed Edge lifecycle: at most two initial commands, idempotent migration/update,
  rollback, doctor/repair, and no repeated pairing when identity is valid.
- Alias-first project/workspace UX: normal chats do not supply `ed_*`, `ws_*`, `mr_*`,
  job, lease, or idempotency identifiers.
- Lossless workspace discovery/association/clone under approved roots; unknown `p12`
  directories remain untouched.
- One durable SQLite scheduler with separate immutable VPS and per-Edge execution pools.
- Initial VPS policy: one active heavy build; isolated 50/65/80 percent CPU calibration;
  whole-builder cgroup/rootless enforcement.
- Edge jobs survive chat/control-plane disconnects without duplicate execution or
  silent VPS fallback.
- VPS builds remain supported; image build/deploy is additive and GitHub Actions is
  optional.
- Hard admission + Deficit Round Robin + deduplication; adaptive priority is shadow-first.

## Evidence and project documentation

- `specs/007-global-work-scheduler/`
- `docs/adr/0004-p16-global-scheduler-separated-execution-pools.md`
- `docs/baselines/2026-07-22-p16-capacity.md`
- `docs/p16_spec_test.go`
- `docs/documentation-map.md`

Measured evidence identifies CPU as the current build bottleneck; RAM, disk, CPU steal,
and monthly transfer were not binding in the captured no-cache deploy.

## Verification

- focused P16 docs test — PASS
- full docs suite passed before this current-task update and must be rerun now
- `git diff --check` — PASS before this current-task update and must be rerun now

## Next step

Restore full green documentation gates, commit
`Step 0: Specify global scheduler and Edge lifecycle`, then begin Step 1 with RED
P12/P15 migration inventory tests. Do not install a worker, alter Coolify, change
production, or touch the real Edge before those tests exist.
