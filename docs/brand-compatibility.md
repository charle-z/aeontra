# Brand and compatibility boundary

Aeontra is the public product name and the public repository slug. The implementation
was developed under the MCP Devbox name, which remains the technical compatibility
identity for the first public release line.

Branding and technical identity change at different speeds. A product-name change must
not silently break clients, installed devices, persisted state, signed updates, import
paths, or operator automation.

## Public presentation

Use **Aeontra** for:

- the product title and short description;
- public project, community, and governance prose;
- future visual identity, website metadata, and release announcements.

Historical evidence may continue to say MCP Devbox when that was the observed name at
the time. Do not rewrite dated baselines, old releases, pull requests, or audit records
to make them appear current.

## Compatibility identifiers

Keep the following stable until a separately reviewed migration provides aliases,
upgrade and rollback behavior, and real install evidence:

- the `github.com/charle-z/mcp-devbox` Go module path;
- the `mcp-devbox` and `mcp-edge` executable names;
- MCP tool names, schemas, routes, protocol identifiers, and catalog semantics;
- `MCP_DEVBOX_*` environment variables;
- `/opt/mcp-devbox`, state roots, package names, systemd units, and service users;
- signed Edge bundle formats, release channels, manifests, and updater contracts;
- persisted database, journal, workspace, worktree, browser, and toolbox layouts.

New documentation may describe these as compatibility identifiers. New internal code
should prefer neutral domain terms when no compatibility contract exists, but must not
duplicate a subsystem merely to avoid an established name.

## Deployment-specific identity

Owner domains, Coolify application identifiers, local usernames, device identifiers,
and private infrastructure values are deployment configuration or historical evidence,
not Aeontra defaults. General installation documentation must use placeholders and
must remain usable without the original maintainer's infrastructure.

## Repository migration and future module rename

The public repository moves from `charle-z/mcp-devbox` to `charle-z/aeontra`. GitHub's
repository redirect preserves existing clones, while new documentation and managed
source operations use the Aeontra URL. Deployment identity validation temporarily
accepts either exact slug under the configured owner so existing Coolify applications
can migrate without downtime.

The Go module and runtime identifiers do not move with the repository. Any future
technical-identity rename is a separate compatibility migration, not a textual
replacement. Its design must address at least:

1. the Go module path and import compatibility;
2. executable, package, service, and state-path aliases;
3. configuration precedence and deprecation windows;
4. signed release and rollback compatibility;
5. OAuth redirect and public-domain configuration;
6. clean installation and in-place upgrade from the compatibility names.

Until that design is implemented and accepted, users install Aeontra from the
`aeontra` repository and operate the `mcp-devbox`/`mcp-edge` compatibility binaries.

## Public release names

Public source and Edge releases use `vMAJOR.MINOR.PATCH` and the title
`Aeontra <version>` or `Aeontra Edge <version>`. The historical `p15.x.y` format remains
valid for installed bundles and the final transition bridge. The mutable `stable` tag
remains a signed machine channel for compatibility; it is not the product version.

Changing package names, service units, state paths or artifact basenames is outside this
version-label migration. Those identifiers continue to use `mcp-devbox` and `mcp-edge`
until an upgrade path includes aliases, package replacement metadata and rollback tests.
