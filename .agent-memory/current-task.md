# Current task

Historical deployed baseline remains unchanged: VPS `main`/production truth is preserved separately; no Parrot mutation occurred in this cut.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Current production and canonical `main` are synchronized at merge commit `ee394ca56f0778d029972694ad13e8166ea5b6a0`. The live runtime reports version `0.2.0`, protocol `2024-11-05`, 102 tools and catalog hash `sha256:5a2091d85585d13eb7efbc22d942b2dfbd71fc7d547581803eb7633cac64d68b`.

Branch: `fix/p16-builder-bootstrap-prereqs`.

## Trigger

PR #48 and the merge commit are fully green and deployed. Before invoking the real VPS bootstrap, review found two clean-host defects:

- the bootstrap assumed `rootlesskit`, `uidmap`, `slirp4netns` and `fuse-overlayfs` were preinstalled;
- its empty root environment used only `/usr/bin:/bin`, while the reviewed installer/calibrator need fixed administrator binaries such as `useradd` and `runuser` under `/usr/sbin`.

Executing the merged bootstrap as-is could therefore fail before calibration despite the intended one-operation host boundary.

## Follow-up candidate

- adds executable zero-argument `packaging/builder/install-prerequisites.sh`;
- supports only Debian or Ubuntu and reads `/etc/os-release` without sourcing it;
- installs only the four literal rootless packages through non-interactive APT when their fixed binaries are missing;
- verifies `rootlesskit`, `newuidmap`, `newgidmap`, `slirp4netns` and `fuse-overlayfs` as non-symlink executables;
- verifies installed package records with `dpkg-query`;
- executes the same prerequisite installer in the disposable BuildKit CI fixture and in the real exact-commit bootstrap;
- fixes the bootstrap to use the immutable root PATH `/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin` before and after transient-systemd reexecution;
- records exact prerequisite package versions in private `host-prerequisites.tsv` calibration evidence;
- preserves installed host prerequisites on rollback instead of removing system packages.

No caller-controlled package, distribution, URL, command, path or resource value was added. No public tool, Docker socket, root shell or production application behavior changed.

## Validation

Green on the exact local tree:

- RED contract tests failed before implementation and pass after it;
- `go test ./... -count=1`;
- `go vet ./...`;
- `go build ./...`;
- Actionlint v1.7.12;
- Staticcheck v0.7.0 on changed Go/test packages;
- POSIX/Bash syntax and executable-mode package tests;
- `git diff --check`.

Next: commit, publish and open a minimal PR; require every exact-head gate green; merge and deploy; then compute the final bootstrap digest. Real 50/65/80 calibration remains the single host-root action that cannot be performed by the non-root public MCP container.
