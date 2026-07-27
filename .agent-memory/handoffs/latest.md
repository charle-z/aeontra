# Handoff — P16 deterministic calibration review

Canonical `main` and production are currently synchronized at
`1e418658102645ed45b739a9a84562704cae65ab`. Coolify application
`jqf7qz5ensoqtvl1tb197gcv` is healthy. The live runtime exposes 102 tools with
catalog hash `sha256:5a2091d85585d13eb7efbc22d942b2dfbd71fc7d547581803eb7633cac64d68b`.

Historical markers remain explicit: P8.1 is closed and deployed at
`d343264bffdc0ae1bc045a9d723e913be977090c`, tagged `p8.1`; that historical
snapshot had 67 tools and Edge state `not_paired`. P9 Brain is its deployed
successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `fix/p16-calibration-review`, based on `origin/main`.

Implemented locally and staged:

- closed deterministic review of the exact 50/65/80 x cached/no-cache evidence matrix;
- bounded cached-log proof requiring `CACHED` for each quota;
- hard rejection of malformed, duplicate, incomplete, failed, unhealthy, OOM,
  unbounded or identity-missing evidence;
- root-private selection output and explicit structural-stop result;
- calibrator/bootstrap integration, tests, docs and accurate Step 7 task state.

Local validation is green: full repository tests, Vet, build, focused builder/docs/
buildspike/coverage tests and diff checks. Staticcheck remains an exact-head CI gate
because the local allowlist does not expose it.

Do not begin Step 8. Real VPS calibration is still a genuine host-root boundary. The
public Coolify MCP has no host systemd/filesystem authority, and privileged profiles are
disabled. After this branch is merged and deployed, run the reviewed one-command
bootstrap against the exact green merge commit, preserve its private archive, record a
new dated baseline, and freeze the selected quota before worker integration.
