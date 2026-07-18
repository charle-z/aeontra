# P12 Linux Workcell MVP

Branch: `p12-linux-workcell-mvp`.
Base: `origin/main` at `087f00e404855cc83e76c1eb7d6ed85ab14577c5`.

Historical deployed foundations preserved:
- P9 Brain: `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.
- P8.1 Console 2.0: `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P11.2 Remote OpenCode Relay is the current Edge foundation.
- Catalog invariant: exactly 85 tools and `sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941`.

Completed commits:
- `03e83bb` — Step 1: trusted local workspace profile/mode metadata, legacy migration, strict roots, typed HTB configuration and CLI.

Step 2 candidate completed:
- versioned and embedded `profiles/htb-linux-v1.md` with authorization, user/root goal, bounded recon, anti-loop, guided enumeration, exploitation, lateral movement, privilege escalation, flags, cleanup, response format, known chain and newly-published-machine rules;
- local HTB preflight validates interface IPv4 and exact target route through the configured VPN interface, deriving LHOST locally;
- idempotent private `.mcp-devbox/{tools,cache,runtime}` plus HTB evidence directories;
- immutable rendered `instructions.md`, writable bounded `current-state.md`, resume without overwrite, atomic writes and symlink-safe parents;
- no host path or HTB secret is added to the VPS contract.

Verification: full Go suite, vet and build pass.

Next action: connect the prepared local workspace contract to OpenCode launcher policy selection while preserving the existing networkless sandbox byte-for-byte in behavior.
