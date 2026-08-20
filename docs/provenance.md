# Contribution provenance

This document records how historical author identities map to the project's copyright
and contribution boundary. It supplements Git history; it does not rewrite authorship,
add co-authors, or replace the Developer Certificate of Origin for future contributions.

## Maintainer identities

Carlos Acosta is the copyright holder and primary maintainer. Historical commits under
the names `Carlos Acosta` and `Charles`, including the maintainer's GitHub noreply and
personal Git identities, are the same maintainer acting through different configured
development environments.

## Owner-directed automation

The maintainer attested on 2026-08-20 that the following historical identities were
automations or agents operating under his direction and that he can license their
contributions:

- `mcp-devbox <mcp-devbox@localhost>` — managed chats and automation used for work on
  the VPS and control plane;
- `edge <edge@mcp-devbox.local>` — managed chats and automation used on the private
  Edge;
- `t <t@t>` — two early managed-agent commits produced under the maintainer's direction.

These identities are provenance labels, not independent maintainers or copyright
holders. The repository does not add artificial `Co-Authored-By` trailers to generated
or agent-assisted work.

## Third-party and future contributions

Vendored, generated, downloaded, and dependency material retains its upstream license
and attribution. [`docs/dependency-licenses.md`](dependency-licenses.md) defines the
artifact-specific review and notice boundary.

Future external contributions use the Developer Certificate of Origin 1.1 described in
[`CONTRIBUTING.md`](../CONTRIBUTING.md). A sign-off certifies the contributor's right to
submit the change under the project's Apache-2.0 license; it does not transfer private
operator state, credentials, or authority.
