# Dependency license policy

Aeontra accepts dependencies only after their licenses and redistribution obligations
are reviewed. The project's own source is licensed under Apache-2.0; dependency metadata
is evidence, not legal advice, and a passing scanner does not license third-party code
under the project's terms.

## Automated gates

The Node workspace uses `pnpm licenses list --json` through
`scripts/check-node-licenses.mjs`. CI rejects a license class outside the reviewed set:

```text
Apache-2.0
BlueOak-1.0.0
BSD-2-Clause
BSD-3-Clause
CC-BY-4.0
CC0-1.0
ISC
MIT
MIT-0
```

The gate separately binds the Creative Commons entries to the expected data packages:

- `caniuse-lite` — CC-BY-4.0, browser compatibility data maintained by the
  Browserslist project and derived from Can I Use;
- `mdn-data` — CC0-1.0, open web data from Mozilla Developer Network.

An unexpected package under either class fails the gate and requires review instead of
silently inheriting the previous approval.

Go dependencies are scanned in CI for vulnerabilities and release images produce an
SBOM. The reviewed Go dependency set currently contains permissive MIT and BSD license
families; changes still require inspection of the exact dependency diff.

Every versioned Dockerfile base and the PostgreSQL rootless fixture retain a readable
tag plus an immutable multi-platform manifest digest. Updating a tag or digest requires
resolving it from the official registry, rebuilding the affected image, and rerunning
the relevant container and host-specific gates.

## Distribution notices

Source packages, container images, Edge packages, browser downloads, and generated web
bundles do not contain identical dependency sets. Each distributable release must keep
its SBOM and the license/notice material required by the components it actually ships.
The repository-level policy is not a substitute for copying required license texts into
a binary or image distribution.

The official Edge workflow generates separate Linux and Windows third-party notice
assets from the exact release commit. It resolves the Go binary dependency graph through
the pinned `go-licenses` module, includes full detected license texts, and adds the
reviewed notices for the pinned Codex and GitHub CLI binaries shipped by Linux. Unknown
licenses fail the release. The notice assets accompany the immutable archives, packages,
checksums, signatures and SBOMs; they do not replace those SBOMs.

The project-level `NOTICE` records Aeontra attribution and compatibility names. It does
not enumerate every component shipped by every artifact. Verify the generated notice
assets on the exact published release before marking the artifact-level gate complete.
The source provenance gate remains satisfied independently through
[`docs/provenance.md`](provenance.md).

## Contributor check

After installing the locked Node dependencies without lifecycle scripts, run:

```bash
pnpm licenses:check
```

Review dependency updates rather than expanding the allowlist automatically.
