# P12 production evidence — Parrot Trusted Linux Workcell

Date: 2026-07-18
Status: merged, deployed, paired, and validated end to end.
P12 merge: `3946fd7033f28906deb932298387034e2fa27fe8`.
Production: CubePath-hosted MCP Devbox control plane and authenticated console.

## Real host evidence

The owner installed the Edge on Parrot WSL2 as a non-root user with systemd,
Bubblewrap 0.11.0, Go 1.26.5, Node 24.18.0, npm 11.16.0, Podman 5.4.2, pinned
OpenCode 1.18.1, the reviewed provider, and the isolated model-turn driver.

Pairing created a local `0700` state root with `0600` identity/key files. The
registered disposable workspace used profile `linux-workcell`, mode `dev`,
and network posture `trusted_host_shared_network`.

Successful runtime:

```text
runtime_id=mr_829f6601fca6f887bc2d0133a4c5dff1
workspace_id=ws_7c4686f5d9244bbad30ae705d4b660c5
state=completed
last_sequence=6
```

The runtime confirmed Git and `README.md`, created the exact requested
`edge-smoke.txt`, returned `?? .mcp-devbox/` and `?? edge-smoke.txt`, and
passed `git diff --check` with exit code zero. No commit, push, dependency install,
container, or service was used.

## Defects exposed by real onboarding

The real host found gaps not covered by the candidate CI matrix:

1. systemd blocked Bubblewrap loopback setup because `AF_NETLINK` was absent from
   `RestrictAddressFamilies`;
2. Bubblewrap verification discarded bounded stderr and reduced the failure to
   `internal`;
3. an early local journal rejection did not always mark the remote runtime failed;
4. terminal goal digests were permanently unique, so a legitimate rerun was rejected;
5. one WSL test rejected `/mnt/wsl/resolv.conf` even though only Windows workspace
   mounts `/mnt/c` and `/mnt/d` are forbidden;
6. provider and systemd onboarding documentation did not match the real tree.

The onboarding hardening branch adds regression tests, the safe
`bubblewrap_netlink_route_denied` diagnostic code,
transactional journal migration, remote failure propagation, the required
`AF_NETLINK` allowance, a packaged OpenCode Edge unit, and an executable Parrot
preflight.

## Honest residual risks

The trusted profile uses a shared host network. It does not enforce target-only
networking or universal egress filtering. A rootless engine socket still grants
broad authority within the Edge user's namespace. The model can modify the entire
selected workspace. These are explicit properties, not hidden limitations.
