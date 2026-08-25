# Native Windows Edge

This document describes the Windows Edge package and its operator boundary. It is
an installation and recovery guide, not evidence that a Windows device has been
installed or accepted.

## State vocabulary

Keep these identities separate:

- **Source candidate:** a commit or branch being built.
- **Signed release:** a versioned Windows bundle with a verified manifest and
  signature.
- **Deployed:** a release installed in the Windows program root and selected by
  `active.json`.
- **Accepted:** a deployed release that has passed the named real-device checks.

A source commit does not prove that a signed release exists. A signed release does
not prove that it is installed. An installed service does not prove acceptance.

## Install and pair

Use the `install-edge.ps1` script from a signed Windows bundle in an elevated
PowerShell session. The script verifies the bundle before creating the service and
rejects non-local paths, reparse points, overlapping roots, and an invalid manifest.
The default managed roots are:

- program files: `Aeontra\Edge`;
- private state: `Aeontra\Edge` under ProgramData;
- registered workspaces: `Aeontra\Workspaces` under ProgramData.

The signed installer also accepts explicit roots on any ready fixed local drive. The
managed layouts are `Aeontra\Edge` for program files, `Aeontra\State` for new private
state roots, and `Aeontra\Workspaces` for registered workspaces. For example:

```powershell
.\install-edge.ps1 -Server https://example.invalid `
  -InstallRoot 'D:\Aeontra\Edge' `
  -StateRoot 'D:\Aeontra\State' `
  -WorkspaceRoot 'D:\Aeontra\Workspaces'
```

The historical `%ProgramData%\Aeontra\Edge` state layout remains valid for existing
installations. UNC paths, device paths, volume roots, removable drives, reparse points
and overlapping roots are rejected. Pass the same explicit roots to the signed
uninstaller when removing a custom installation. The updater and doctor derive the
selected install root from their active signed binary and read the protected state and
workspace roots from `service-config.json`; they do not trust caller-controlled path
environment variables.

The installer creates the `AeontraEdge` SCM service under the virtual account
`NT SERVICE\AeontraEdge`. Pairing uses one prompted, one-shot code. The code is
written to a private request file, consumed by the service, and removed after use.
It is not accepted as a command-line argument.

The service configuration is server-owned. `service-config.json` records only the
service identity and the managed state/workspace roots. `active.json` selects one
immutable release directory. Both are checked by the Windows doctor.

## Inspect and recover

Run the installed binary from an elevated or operator PowerShell prompt:

```powershell
& "$env:ProgramFiles\Aeontra\Edge\releases\<release>\bin\mcp-edge.exe" doctor
& "$env:ProgramFiles\Aeontra\Edge\releases\<release>\bin\mcp-edge.exe" lifecycle inspect
```

Replace the program root in these commands when the package was installed on another
fixed drive.

The checks are read-only. They verify the signed Windows manifest, active release,
service configuration, SCM service identity and binary command, paired identity,
and disjoint managed roots. They return bounded status and do not print paths,
pairing material, private identity fields, or service credentials.

`doctor --repair` does not mutate the installation. It reports that the signed
`mcp-bundle-updater.exe` is the only supported repair path.

## Update and rollback

Updates and rollback are direct invocations of the signed updater, never a shell or
caller-selected executable:

```powershell
& "$env:ProgramFiles\Aeontra\Edge\releases\<release>\bin\mcp-edge.exe" lifecycle update stable
& "$env:ProgramFiles\Aeontra\Edge\releases\<release>\bin\mcp-edge.exe" lifecycle rollback
```

The updater verifies the signed stable channel, archive digest, Windows manifest,
platform and architecture before staging an immutable versioned release. It keeps
the prior marker until the new service starts and restores it if startup or identity
validation fails. The update lock prevents concurrent transactions.

## Uninstall and cleanup

Use the signed `uninstall-edge.ps1` script. It stops and removes only the managed
`AeontraEdge` service and installed program releases. State and registered workspaces
remain by default. `-RemoveData` is an explicit destructive operation that also
removes the paired identity and workspaces; back up the state before using it. Supply
the installed `-InstallRoot`, `-StateRoot`, and `-WorkspaceRoot` for a custom layout.

## Boundary and limitations

Native Windows workspaces use trusted host-shared execution with Windows ACL and
Job Object controls. This is not the networkless Linux Edge sandbox. The Windows
package currently does not claim the Linux rootless toolbox, browser harness, or
HTB workflow on the native host. Those capabilities require their separately
validated profiles and acceptance evidence.

Source, release, deployment and real-device acceptance must be recorded separately
in dated evidence. Do not copy mutable commit, release, tool-count or catalog values
into this guide.
