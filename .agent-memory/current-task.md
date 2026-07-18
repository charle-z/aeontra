# P12 Linux Workcell MVP

Branch: `p12-linux-workcell-mvp`.
Base: `origin/main` at `087f00e404855cc83e76c1eb7d6ed85ab14577c5`.

Foundations preserved:
- P9 Brain `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.
- P8.1 Console 2.0 `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P11.2 Remote OpenCode Relay remains the base.
- Catalog remains 85 tools with `sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941`.

Completed commits:
- `03e83bb` — Step 1: trusted workspace metadata and local CLI.
- `901ff9b` — Step 2: durable local context, HTB template and VPN/route preflight.

Step 3 candidate completed:
- OpenCode resolves both opaque path and trusted workspace record locally, re-resolves after verification, and rejects path/profile/mode/HTB metadata drift;
- unchanged `sandbox` still requires `--unshare-all` and explicitly rejects host-shared networking;
- `linux-workcell` derives from the validated P11.2 baseline but adds explicit `--share-net`, persistent workspace-local package prefixes, bounded system networking files and local TARGET/LHOST only in HTB mode;
- read-only mounts are exact-allowlisted, preventing `.ssh`, browser or other host data leaks;
- full launcher execution test confirms policy selection from the local registry.

Verification: full Go suite, vet and build pass.

Next action: implement safe local tool inventory plus optional user-owned rootless Docker/Podman socket support and runtime-labelled cleanup. No rootful Docker socket will be accepted.
