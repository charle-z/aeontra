# Signed versioned Edge bundles

P15 distributes the Parrot Edge as one indivisible release rooted at
`/opt/mcp-devbox/releases/<RELEASE>`. `/opt/mcp-devbox/current` is an atomic symlink
to the active release; compatibility paths under `/usr/local` and
`/opt/mcp-devbox/opencode-provider` point into that release and are managed only by
the package/updater.

## Trust and manifest contract

`manifest.json` is canonical JSON signed with Ed25519. `manifest.sig` is the raw
64-byte signature. The private release key is never installed on Edge devices. The
public trust key, release, commit and expected catalog hash are compiled into packaged
executables; an unstamped local build cannot validate a production bundle.

The current version-4 manifest binds:

- release, exact 40-character Git commit, bundle protocol and architecture;
- the deterministic exterior MCP catalog hash;
- SHA-256 hashes for `mcp-edge`, `model-turn-driver`, the reviewed Node 24.18.0 runtime,
  `mcp-autopilot-worker`, the privileged updater, bundled GitHub CLI, OpenCode and its
  lockfile, pinned stock Codex and its exact pin manifest, provider `index.js`, provider
  `htb-actions.js`, provider `dev-actions.js`, provider `package.json`, and the packaged
  Edge systemd unit.

The verifier retains all exact signed historical layouts. Version 1 predates
`dev-actions.js`; version 2 adds and hashes it; version 3 adds the bundled GitHub CLI;
version 4 adds and hashes Codex plus its pin manifest. An installed version-3 updater
cannot validate version 4, so the first migration uses a signed version-3 bridge whose
new updater understands both versions, followed by the signed version-4 Codex bundle.
Other manifest versions fail closed.

Every component must be a regular non-symlink file below the release root. Unknown,
missing, extra or malformed manifest fields fail closed. The Edge verifies the bundle
before polling for a new runtime, so a partial or mixed installation never discovers
missing tools after work has started.

Safe failure codes are intentionally closed:

| Code | Meaning |
|---|---|
| `manifest_invalid` | malformed manifest, invalid trust key or bad signature |
| `bundle_mismatch` | release, commit, protocol, architecture, catalog or core component mismatch |
| `provider_outdated` | provider component missing or hash mismatch |
| `driver_outdated` | model-turn driver missing or hash mismatch |

No code includes a path, hash value, key, target, command, credential or provider
configuration.

## Release generation

The release pipeline stages the selected fixed layout, then invokes
`mcp-bundle-manifest` with an absolute release root, manifest version, the exact
release/commit/protocol/catalog/architecture and an absolute raw Ed25519 private-key
file. The command creates new manifest/signature files only; it refuses overwrite.
`bridge-v3` retains the OpenCode unit and omits Codex; `codex-v4` hashes the pinned Codex
files and activates the Codex unit. Debian packaging and the privileged updater consume
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
