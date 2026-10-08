# Public alpha

The public alpha is for a single operator evaluating Aeontra against repositories they
control. It is not a hosted multi-user service. Start with a disposable repository,
keep the server local, and grant only the authority needed for the test.

No service-level agreement, compatibility guarantee, or managed recovery service is
provided. The latest release and `main` may move independently; report both identities
when something fails.

## Choose a setup

| Goal | What you run |
|---|---|
| **Local MCP client** | Run Aeontra beside the client over `stdio`. No Edge, public URL, OAuth, or VPS is required. |
| **ChatGPT + remote control plane** | Run one operator-owned Aeontra control plane behind a stable public HTTPS hostname with OAuth and persistent state. Add its clean `/mcp` URL to that operator's ChatGPT account. |
| **ChatGPT + Edge on your computer** | Use the previous control plane, then install and pair the signed Edge on Windows, Linux, Parrot, or WSL. The Edge connects outbound; the computer does not need an inbound public port. |

The current product is single-operator. Each person runs and configures their own
control plane, OAuth owner identity, ChatGPT MCP connection, Edge devices, policies,
and repository roots. One deployment must not be shared by unrelated users because
Aeontra does not yet provide multitenant isolation.

The supported remote path uses a stable HTTPS hostname rather than a bare IP address.
The maintainer's `aeontra.com` site and public runtime indicator are product evidence,
not a shared connector for visitor repositories.

## Ten-minute local trial

Requirements:

- Go 1.26;
- an MCP client that can start a local stdio server;
- an absolute path to a disposable repository.

Install the compatibility executable from the latest tagged source release:

```bash
go install github.com/charle-z/mcp-devbox/cmd/mcp-devbox@latest
```

Start it in read-only mode:

```bash
mcp-devbox serve \
  --root /absolute/path/to/disposable-repository \
  --mode read-only
```

Configure the client with the installed binary and the same absolute path. The common
MCP client shape is:

```json
{
  "mcpServers": {
    "aeontra": {
      "command": "/absolute/path/to/mcp-devbox",
      "args": [
        "serve",
        "--root",
        "/absolute/path/to/disposable-repository",
        "--mode",
        "read-only"
      ]
    }
  }
}
```

Use the platform-appropriate executable path. On Windows, the installed executable is
normally `mcp-devbox.exe` under the Go binary directory.

## First acceptance

After the client connects:

1. call `system_runtime_info` and retain the returned source/catalog identity;
2. call `workspace_checkpoint` for the disposable repository;
3. read one known non-secret file;
4. confirm that a path outside the configured root is rejected;
5. stop the server and confirm that no repository file changed.

This proves the local read-only path. It does not prove the HTTP/OAuth deployment,
GitHub or Coolify authority, a signed Edge installation, or a real-device workcell.

## Add reviewed writes

Change to `ask` only after the read-only trial. Configure the smallest command and test
allowlists needed by the disposable repository. The complete options and precedence
remain canonical in [`configuration.md`](configuration.md); the trust boundaries and
known limitations remain canonical in [`security.md`](security.md).

## Advanced paths

- Connect a remote HTTPS deployment to ChatGPT: [`connect-remote.md`](connect-remote.md).
- Deploy the control plane behind TLS: [`deploy-coolify.md`](deploy-coolify.md).
- Install a signed Windows Edge: [`install-edge-windows.md`](install-edge-windows.md).
- Install a signed Linux/WSL Edge: [`install-edge-linux.md`](install-edge-linux.md).

These paths require operator-owned infrastructure, pairing, credentials, and explicit
policy. The maintainer demo does not provide that authority to visitors.

## Feedback

Use GitHub Issues and select **Public alpha feedback**. Include the smallest redacted
reproduction, operating system, client, source or release identity, and the point where
the guide became unclear. Do not attach credentials, private URLs, OAuth material,
repository contents, or raw production logs.

Use [`SECURITY.md`](../SECURITY.md) and private vulnerability reporting for suspected
security defects. Do not disclose an unpatched vulnerability through the feedback form.
