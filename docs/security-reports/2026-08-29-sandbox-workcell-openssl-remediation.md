# Sandbox workcell OpenSSL remediation — 2026-08-29

## Trigger

The `Container SBOM and vulnerabilities` job for PR #248 built the sandbox
workcell successfully and generated its SBOM. Grype 0.110.0 then rejected the
image because the pinned Wolfi base contained `libcrypto3` and `libssl3`
`3.6.3-r5`. The current vulnerability database reported multiple High findings,
including `CVE-2026-14457`, `CVE-2026-18798`, `CVE-2026-54874`,
`CVE-2026-63072`, `CVE-2026-63075`, and `CVE-2026-63076`.

## Remediation

`Dockerfile.sandbox-workcell` now uses the Wolfi multi-platform image index
published on 2026-08-28:

```text
sha256:03c6561658909fc4eadd0b2dc717375df40a22cc05455b8f82f1f1974e7e4427
```

The updated base contains `libcrypto3` and `libssl3` `3.6.4-r0`. All explicit
Go, Node.js, npm, Python, Rust, and utility package pins remain unchanged. The
workflow policy test binds the reviewed base digest.

Official image metadata:

- <https://images.chainguard.dev/directory/image/wolfi-base/overview>
- <https://github.com/chainguard-images/images/tree/main/images/wolfi-base>

## Candidate evidence

The candidate image was built with rootless Podman from the exact Dockerfile.
The build installed all 71 pinned dependencies and reported the expected
OpenSSL package versions. Grype 0.110.0 was downloaded from its official GitHub
release, verified with the published checksum, and run with the repository's
unchanged High threshold. Both Grype and `cmd/grype-gate` passed.

No vulnerability allowlist, VEX override, severity change, ignored fix state,
or `continue-on-error` exception was added. GitHub exact-head Actions remain the
authoritative publication gate.
