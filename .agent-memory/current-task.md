# P12 Linux Workcell MVP

Branch: `p12-linux-workcell-mvp`.
Base: `origin/main` at `087f00e404855cc83e76c1eb7d6ed85ab14577c5`.

Historical deployed foundations preserved:
- P9 Brain is deployed at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.
- P8.1 Console 2.0 is deployed and tagged `p8.1` at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P11.2 Remote OpenCode Relay is deployed and remains the Edge foundation.
- Catalog remains exactly 85 tools with `sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941`.

Completed commits:
- `03e83bb` — Step 1: trusted workspace metadata and local CLI.
- `901ff9b` — Step 2: durable local context, HTB template and VPN/route preflight.
- `bae1904` — Step 3: isolated policy selection with unchanged sandbox and explicit host-shared-network workcell.

Step 4 candidate completed:
- local tool inventory reports only name, availability, sanitized version and capability; it is available through `workspace inventory` and persisted as `.mcp-devbox/tool-inventory.json`;
- rootless Docker/Podman discovery accepts only a user-owned Unix socket under `/run/user/<uid>` and never rootful Docker sockets;
- the workcell receives the rootless socket, exact runtime label and engine environment only when the safe endpoint exists;
- containers, networks and volumes are listed and removed only through the exact `mcp.devbox.runtime=<runtime_id>` label;
- engine output is bounded and resource identifiers are validated before cleanup;
- completion, cancellation, timeout and failure write a bounded terminal checkpoint to `current-state.md` after the process group stops;
- no paths from inventory are sent to the VPS and no MCP tool was added.

Targeted edgeclient and CLI suites pass. Full suite initially exposed only a historical-memory wording mismatch; this checkpoint restores the explicit P8.1 deployed state required by the documentation gate.

Next action: rerun full suite, vet/build/race and commit Step 4; then synchronize architecture/security/Parrot setup docs and add final E2E fixture coverage.
