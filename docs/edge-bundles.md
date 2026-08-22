# Signed versioned Edge bundles

Aeontra distributes the Parrot Edge as one indivisible release rooted at
`/opt/mcp-devbox/releases/<RELEASE>`. `/opt/mcp-devbox/current` is an atomic symlink
to the active release. Compatibility paths under `/usr/local` are managed only by the
package/updater. Version-5 Codex-only releases remove the historical OpenCode, provider,
Node and model-turn-driver links; retained signed v4 releases remain available for an
explicit rollback.

## Trust and manifest contract

`manifest.json` is canonical JSON signed with Ed25519. `manifest.sig` is the raw
64-byte signature. The private release key is never installed on Edge devices. The
public trust key, release, commit and expected catalog hash are compiled into packaged
executables; an unstamped local build cannot validate a production bundle.

The current version-5 manifest binds:

- release, exact 40-character Git commit, bundle protocol and architecture;
- the deterministic exterior MCP catalog hash;
- SHA-256 hashes for `mcp-edge`, `mcp-autopilot-worker`, the privileged updater,
  bundled GitHub CLI, pinned stock Codex and its exact pin manifest, the neutral Edge
  systemd unit and its onboarding path unit.

The verifier retains all exact signed historical layouts. Version 1 predates
`dev-actions.js`; version 2 adds and hashes it; version 3 adds the bundled GitHub CLI;
version 4 adds and hashes Codex plus its pin manifest while retaining the OpenCode
rollback harness; version 5 removes the OpenCode-only components and changes the active
unit to `mcp-devbox-edge@.service`. An installed v4 updater cannot validate v5. The
transition therefore installs one v4 bridge built from the v5-aware source before the
v5 release. Other manifest versions fail closed.

Every component must be a regular non-symlink file below the release root. Unknown,
missing, extra or malformed manifest fields fail closed. The Edge verifies the bundle
before polling for a new runtime, so a partial or mixed installation never discovers
missing tools after work has started.

The updater returns this closed set of safe failure codes:

| Code | Meaning |
|---|---|
| `manifest_invalid` | malformed manifest, invalid trust key or bad signature |
| `bundle_mismatch` | release, commit, protocol, architecture, catalog or core component mismatch |
| `provider_outdated` | provider component missing or hash mismatch |
| `driver_outdated` | model-turn driver missing or hash mismatch |

No code includes a path, hash value, key, target, command, credential or provider
configuration.

## Release generation

Release identifiers use one of two closed formats:

- `p15.x.y` identifies the historical line and the final compatibility bridge;
- `vMAJOR.MINOR.PATCH` identifies public Aeontra releases and follows stable SemVer
  numeric components without prerelease or build suffixes.

The `stable` tag is a mutable machine channel, not a bundle version. It contains only
the signed channel document and signature. The channel names one immutable release,
and the updater downloads that release's signed archive. Existing clients therefore
continue to use `update stable` across the version-name transition.

The release pipeline stages the selected fixed layout, then invokes
`mcp-bundle-manifest` with an absolute release root, manifest version, the exact
release/commit/protocol/catalog/architecture and an absolute raw Ed25519 private-key
file. The command creates new manifest/signature files only; it refuses overwrite.
`bridge-v3` retains the historical OpenCode unit and omits Codex; `codex-v4` is the
rollback-compatible updater bridge; `codex-v5` contains only the active Codex harness
and the neutral Edge unit. Debian packaging and the privileged updater consume
this already signed staged directory and never accept caller-provided URLs, paths,
hashes or scripts.

Do not hand-edit a manifest, copy one component between releases, or repoint `current`
outside the reviewed installer/updater transaction.

The stable update channel is a separate canonical JSON document signed by the same
release trust key. It contains only version, release, commit, protocol, catalog,
architecture and archive SHA-256. The archive URL is derived from the compiled official
release base and signed release name; it is never an input to a public tool. See
`docs/install-edge-parrot-p15.md`.

The official channel signature proves artifact identity; a separate durable server
control signature proves that the paired device was actually assigned the closed
`update stable`, rollback or repair operation. Edge verifies both boundaries before
the privileged fixed unit can run.

## Codex-only transition

The v4-to-v5 transition uses this order:

1. publish and install one `codex-v4` bridge from the exact v5-aware source commit;
2. verify that its updater, active Codex runtime and retained v4 rollback layout work;
3. publish one `codex-v5` release from the same green source line and advance `stable`;
4. update the real Edge, verify the neutral unit and absence of OpenCode components;
5. roll back once to the retained v4 bridge, then update forward to v5 and repeat the
   Codex runtime acceptance.

Keep the v4 bridge until every supported device has crossed the manifest boundary.
