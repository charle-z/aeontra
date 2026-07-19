# Plan — HTB lab autonomy

1. Add an idempotent `mcp-edge lab init` flow that creates or reuses the Git workspace, applies `htb-linux` metadata, validates the VPN route, and reports the opaque workspace ID.
2. Add a private HTB lab broker owned by the Edge process. The model sends only username, artifact handle, extraction prefix, and remote command; the broker fixes the registered target and keeps the recovered password outside Bubblewrap and the control plane.
3. Support local-only output capture for flags and other sensitive results, returning only path, byte count, and SHA-256.
4. Sanitize HTB checkpoints before they are embedded into model turns. Preserve local handles and statuses while removing passwords, tokens, and flag-like values.
5. Teach the HTB profile to use non-root `nmap -sT -Pn` and brokered SSH rather than abandoning a confirmed credential chain.
6. Preserve bans on operator credentials, arbitrary targets, CIDRs, rootful Docker, Windows mounts, solution lookup, and telemetry leakage.
7. Run focused tests, full Go tests, vet, staticcheck, build, diff checks, exact-head CI, and a real continuation of the existing Cap workspace.
8. After this local one-command flow is proven, design a separate signed remote bootstrap protocol if the operator should literally do nothing beyond connecting the VPN.
