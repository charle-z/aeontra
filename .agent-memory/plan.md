# P11.2 execution plan

Branch: `p11-2-remote-opencode-relay`, based exactly on `origin/main` commit `01fde5067752ab1c43424d2d54f9afd914617ba5`.

1. Extend authoritative model-runtime persistence with Edge device/workspace binding, distributed states, heartbeat, expiry, opaque refs and idempotency.
2. Add a private local workspace registry and closed `mcp-edge workspace add|list|remove` commands.
3. Add signed device-bound Edge runtime/turn relay endpoints over the existing Ed25519/nonce protocol.
4. Add `RemoteEdgeTransport` to the local model-turn driver with a minimal durable idempotency journal.
5. Add a structured pinned OpenCode launcher with local provider profiles, Unix socket isolation, lifecycle control and bounded redacted output.
6. Add bounded MCP controls for requesting/status/cancelling remote OpenCode runtimes; preserve historical catalog hashes.
7. Build a true distributed E2E with separate authoritative server, Edge relay, driver and OpenCode processes/containers; validate normal flow and restart/resume.
8. Add security/egress evidence and Parrot WSL installation documentation.
9. Run all local and GitHub gates, commit a verified report, publish only this branch and open a PR without merge or deployment.

Capability probe: the currently loaded ChatGPT connector manifest does not expose `mcp_client_capabilities`; `sampling_supported` remains undetermined. `PullRendezvousTransport` remains production transport and `MCPSamplingTransport` remains inactive.
