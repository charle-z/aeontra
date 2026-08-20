# Container runtime remediation — 2026-08-20

## Scope and triggering evidence

The `Container SBOM and vulnerabilities` job on draft PR #200 failed at source
commit `e12e99ac239823b1da1574f5806a25c22ccf3ab8`. SBOM generation completed; the
blocking result came from the repository's High/Critical Grype enforcement:

- `CVE-2026-14456`, High, `libcrypto3` `3.5.7-r0`;
- `CVE-2026-14456`, High, `libssl3` `3.5.7-r0`.

The affected packages came from the Alpine 3.24 final runtime images. OpenSSL's
3.5 branch fixes the issue after 3.5.7; Alpine 3.21 remains supported through
2026-11-01 and carries the unaffected OpenSSL 3.3 branch.

Primary references:

- <https://openssl-library.org/news/secadv/20260813.txt>
- <https://github.com/openssl/openssl/commit/08e7756c3900bcfd77a720e7b74e27d6e4ed01a9>
- <https://www.alpinelinux.org/releases/>

## Remediation

All four final runtime images now use the exact Alpine 3.21 multi-platform image
index:

```text
sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
```

Each runtime upgrades the pinned repository snapshot before installing its
minimal package set. Healthchecks call the BusyBox applet explicitly, and the
fixed numeric service UID/GID contracts remain unchanged.

The main image preserves its required Go 1.26.6, Node 22.23.2 and npm 12.0.1
toolchains without inheriting Alpine's vulnerable Node/SQLite dependency graph:

- the Go toolchain is copied from the pinned Go build stage;
- the Node project's musl artifacts for amd64 and arm64 are selected by
  `TARGETARCH` and verified by architecture-specific SHA-256;
- npm and its previously remediated dependency tree remain checksum-pinned.

The validation runner was missing from the image scan/SBOM matrix. Adding it
found four High Go standard-library findings in the Docker 29.7.2 CLI inherited
from the official image, which had been built with Go 1.26.5. The runner now
builds that same Docker CLI release from its official tag using Go 1.26.6. The
source is bound by:

- tag: `v29.7.2`;
- commit: `a7dcaa6fdb6ed04aacbfdc76357fdae01605609e`;
- source archive SHA-256:
  `225b7ab2a15f5230b482df8461069cd4bce38891266fb9898d4188d0a3cbf54a`.

The build fails unless Go metadata identifies the produced CLI as Go 1.26.6.
Docker CLI's `LICENSE` and `NOTICE` are retained in the final image.

No severity threshold, finding allowlist, VEX override, `only-fixed` filter or
`continue-on-error` exception was introduced.

## Candidate evidence

The exact working diff was validated with bounded concurrency in a native Linux
checkout:

- all four Docker images built with rootless Podman;
- four SPDX JSON SBOMs were generated and non-empty;
- all four Grype reports passed the repository's unchanged High/Critical gate;
- the main image reported Go 1.26.6, Node 22.23.2, npm 12.0.1 and UID/GID 10001;
- the validation runner reported Docker CLI 29.7.2 built by Go 1.26.6;
- process, fixed UID and container health smoke checks passed;
- `go test -p 1 ./... -count=1`, `go vet -p 1 ./...`,
  `go build -p 1 ./...`, catalog verification, formatting and
  `git diff --check` passed.

One test invocation from a Windows-hosted worktree mounted into WSL failed
because Git for Linux could not resolve the Windows path stored in the worktree's
`.git` file. The same `packaging/builder` test passed on both the unmodified base
and the candidate diff in a native Linux checkout. It is therefore classified as
a checkout-transport limitation, not a product regression.

GitHub exact-head Actions remain the authoritative publication gate. This report
must not be treated as final CI closure until the new PR head completes all checks.
