# Dependency license policy

Aeontra accepts dependencies only after their licenses and redistribution obligations
are reviewed. Dependency metadata is evidence, not legal advice, and a passing scanner
does not license the project's own source.

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

## Distribution notices

Source packages, container images, Edge packages, browser downloads, and generated web
bundles do not contain identical dependency sets. Each distributable release must keep
its SBOM and the license/notice material required by the components it actually ships.
The repository-level policy is not a substitute for copying required license texts into
a binary or image distribution.

Before the first licensed public release, generate the release artifacts from the exact
candidate commit, inspect their SBOMs, and add a release-level `NOTICE` or equivalent
third-party notice bundle. Do not add a project `NOTICE` that implies the project's own
copyright or license has been cleared before the provenance gate is complete.

## Contributor check

After installing the locked Node dependencies without lifecycle scripts, run:

```bash
pnpm licenses:check
```

Review dependency updates rather than expanding the allowlist automatically.
