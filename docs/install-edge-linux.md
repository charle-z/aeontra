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

Fresh onboarding validates the HTTPS origin before reading stdin and prints
`Pairing code (stdin):`. Piped codes remain supported and the command never echoes
the code. Reused identities do not read stdin or prompt. If service activation times
out, the paired identity remains available: diagnose with `mcp-edge doctor`, then rerun
onboarding without creating another code. Completion reports bundle validation and
service activation; local doctor and remote capability checks remain separate.

The CLI executes the full preflight embedded in its verified signed binary, with a
45-second deadline. Updating the signed bundle also updates this check; an older
Debian package helper cannot select an obsolete harness. The installer retains the
same reviewed preflight source for its initial package checks.

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

After registering a disposable development project, execute one harmless command through
`project_exec` as a separate workcell check. `mcp-edge doctor` verifies the installed
service and bundle, but does not prove that a child Bubblewrap process can create its
filesystem namespace. If the command reports
`bwrap: Can't open source /usr: Function not implemented`, check the installed unit's
`RestrictSUIDSGID` setting and update to a signed release that permits Bubblewrap's
`openat2` call. Do not hand-edit the packaged unit or bypass Bubblewrap.

## Update, rollback, and removal

### WSL availability after closing terminals

WSL systemd services do not keep the distribution running by themselves. For a
workstation Edge that should remain available while its Windows owner is logged in,
use the optional host-side installer from the reviewed source checkout:

```powershell
.\scripts\edge-wsl-availability.ps1 -Mode Plan -Distribution parrot -LinuxUser charles
.\scripts\edge-wsl-availability.ps1 -Mode Install -Distribution parrot -LinuxUser charles -Root 'D:\Aeontra\Wsl'
.\scripts\edge-wsl-availability.ps1 -Mode Status -Distribution parrot -LinuxUser charles
```

Select the actual distribution, Linux Edge user and an installation directory on any
suitable disk. The example drive is not a product default. Installation runs Edge
doctor first and registers one limited, current-user logon task. Its hidden Windows
process runs only `wsl.exe --exec /usr/bin/sleep infinity`; systemd still owns the
single Edge daemon. Three bounded restart attempts handle an unexpected task exit.
No Windows administrator credentials, service restart, distro shutdown, global WSL
configuration or networking change is required.

Rollback removes only the exact identity-bound task; its small installed script is
retained for inspection:

```powershell
.\scripts\edge-wsl-availability.ps1 -Mode Remove -Distribution parrot -LinuxUser charles
```

This is an opt-in workstation lifetime anchor, not remote wake-on-LAN. Logout, Windows
sleep/shutdown and an explicit WSL termination can still interrupt availability. Check
`edge_connectivity_status` after reconnecting; reconcile durable operations before
retrying effects. Microsoft documents the lifetime limitation in its
[WSL systemd guide](https://learn.microsoft.com/en-us/windows/wsl/systemd).

### Signed component updates

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
