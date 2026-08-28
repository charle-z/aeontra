# Linux Edge installation

This guide is the shortest supported installation path for a signed Aeontra Edge on an
amd64 Debian-compatible host, including Parrot and a WSL2 distribution with systemd.
It assumes that the operator already runs an HTTPS control plane with persistent
`/state`. The Edge makes an outbound connection; no inbound host port is required.

Source, signed release, installed service, paired identity, and real-device acceptance
are separate facts. Check each one before granting project authority.

## 1. Download and verify

Open the [latest Aeontra release](https://github.com/charle-z/aeontra/releases/latest)
and download the amd64 Debian package, its checksum/signature material, SBOM, and
third-party notice. Use the release whose identity the operator intends to support.

Verify the published checksum and signed manifest before installation. Do not install
an archive copied from another machine or extracted from an unverified workflow run.

## 2. Install

Run the package command from the intended non-root Edge user's login session:

```bash
sudo apt install ./mcp-devbox-edge_<version>_amd64.deb
```

The package installs the immutable signed release, fixed systemd units, restricted
updater, pinned runtime components, and required rootless dependencies. It does not
pair the device or select a repository.

For unattended installation, explicit user selection, package contents, and the full
lifecycle contract, see
[`install-edge-parrot-p16.md`](install-edge-parrot-p16.md). Do not hand-edit the unit,
active release marker, or bundle contents.

## 3. Create one pairing code

In a private terminal of the operator's control-plane container, create one short-lived
code:

```bash
mcp-devbox edge pairing-create --state-root /state --ttl 10m
```

Do not put the code in Git, a prompt, a URL, process arguments, or shell history.

## 4. Pair and start

Back in the Edge user's terminal, run:

```bash
mcp-edge onboard --server https://mcp.example.com
```

Enter the one-time code only when prompted. Replace the example origin with the
operator's stable HTTPS control plane. Re-running onboarding after successful pairing
reuses the existing identity and must not request a new code.

## 5. Verify

Run the local read-only checks:

```bash
mcp-edge doctor
systemctl is-active "mcp-devbox-edge@${USER}.service"
```

Then use the control plane to call `edge_onboarding_status` and `edge_bundle_status` for
the human Edge alias. Accept the host only when the signed bundle is valid, the service
is active, one daemon owns the lock, and the reported release matches the intended
installation. Platform-specific capabilities must report their own status; do not
infer toolbox or browser acceptance from service health.

## Update, rollback, and removal

Request updates and rollback only through the paired control plane's closed operations:

```text
edge_bundle_update(release="stable")
edge_bundle_rollback
```

The Edge delegates these operations to its restricted signed updater. An update and a
rollback are separate operational changes and require their own health checks. The
package and updater preserve the previous signed release according to the documented
retention boundary. See [`edge-bundles.md`](edge-bundles.md) for signatures, channel
metadata, compatibility layouts, and failure codes.

Use the package manager and documented lifecycle procedure for removal. Preserve the
private Edge state unless the operator has explicitly decided to destroy the paired
identity and registered workspaces.
