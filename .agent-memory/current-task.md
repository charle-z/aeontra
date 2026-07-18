# P12 Linux Workcell MVP

Branch: `p12-linux-workcell-mvp`.
Base: `origin/main` at `087f00e404855cc83e76c1eb7d6ed85ab14577c5`.

Historical deployed foundations preserved:
- P9 Brain is deployed at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.
- P8.1 Console 2.0 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P11.2 Remote OpenCode Relay is part of the current main foundation.
- Current catalog invariant remains exactly 85 tools with `sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941`.

Immediate objective: implement one opt-in `linux-workcell` profile for Parrot WSL with default `dev` mode and optional local `htb-linux` context. Preserve the existing `sandbox` behavior and do not add an MCP tool.

Completed in Step 1 candidate:
- local SQLite registry now stores trusted profile/mode metadata in a separate table;
- legacy rows migrate to `sandbox` + `dev` without changing opaque IDs;
- `linux-workcell` registration is explicit and limited to local Linux workspace roots;
- `workspace configure` accepts typed dev/HTB metadata;
- target is a single IPv4, OS is LINUX, difficulty is bounded, and Windows mounts/symlinks remain rejected;
- targeted registry and CLI tests pass.

Next action: verify full suite/vet, commit Step 1, then implement local preflight, rendered instructions/current-state, and HTB directory structure.
