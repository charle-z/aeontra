[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^https://')]
    [string]$Server,

    [ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$')]
    [string]$Name = 'windows-edge',

    [SecureString]$PairingCode,

    [string]$SourceBinary = (Join-Path $PSScriptRoot 'bin\mcp-edge.exe'),
    [string]$InstallRoot = (Join-Path ([Environment]::GetFolderPath([Environment+SpecialFolder]::ProgramFiles)) 'Aeontra\Edge'),
    [string]$StateRoot = (Join-Path ([Environment]::GetFolderPath([Environment+SpecialFolder]::CommonApplicationData)) 'Aeontra\Edge'),
    [string]$WorkspaceRoot = (Join-Path ([Environment]::GetFolderPath([Environment+SpecialFolder]::CommonApplicationData)) 'Aeontra\Workspaces')
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$serviceName = 'AeontraEdge'
$serviceIdentity = 'NT SERVICE\AeontraEdge'
$pairRequest = Join-Path $StateRoot 'pair-request.json'
$serviceConfig = Join-Path $InstallRoot 'service-config.json'

function Assert-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'Aeontra Edge installation requires an elevated PowerShell session.'
    }
}

function Resolve-LocalAbsolutePath([string]$Path, [string]$Label) {
    if ([string]::IsNullOrWhiteSpace($Path)) {
        throw "$Label is required."
    }
    $full = [IO.Path]::GetFullPath($Path)
    $root = [IO.Path]::GetPathRoot($full)
    if ([string]::IsNullOrWhiteSpace($root) -or $root.StartsWith('\\') -or $full.StartsWith('\\?\') -or $full.StartsWith('\\.\')) {
        throw "$Label must be an absolute path on a local drive."
    }
    if ($full.TrimEnd('\') -eq $root.TrimEnd('\')) {
        throw "$Label must not be a volume root."
    }
    return $full.TrimEnd('\')
}

function Test-PathOverlap([string]$Left, [string]$Right) {
    $leftPath = $Left.TrimEnd('\')
    $rightPath = $Right.TrimEnd('\')
    return $leftPath.Equals($rightPath, [StringComparison]::OrdinalIgnoreCase) -or
        $leftPath.StartsWith($rightPath + '\', [StringComparison]::OrdinalIgnoreCase) -or
        $rightPath.StartsWith($leftPath + '\', [StringComparison]::OrdinalIgnoreCase)
}

function Assert-NoReparsePath([string]$Path, [string]$Label) {
    $full = [IO.Path]::GetFullPath($Path).TrimEnd('\')
    $root = [IO.Path]::GetPathRoot($full)
    $current = $root.TrimEnd('\')
    foreach ($part in $full.Substring($root.Length).Split('\', [StringSplitOptions]::RemoveEmptyEntries)) {
        $current = Join-Path $current $part
        if (Test-Path -LiteralPath $current) {
            $item = Get-Item -LiteralPath $current -Force
            if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
                throw "$Label contains a reparse point."
            }
        }
    }
}

function Set-PrivateDirectoryAcl([string]$Path, [Security.AccessControl.FileSystemRights]$ServiceRights) {
    $serviceAccount = [Security.Principal.NTAccount]::new($serviceIdentity)
    $systemAccount = [Security.Principal.NTAccount]::new('NT AUTHORITY\SYSTEM')
    $administrators = [Security.Principal.NTAccount]::new('BUILTIN\Administrators')
    $inherit = [Security.AccessControl.InheritanceFlags]'ContainerInherit, ObjectInherit'
    $propagation = [Security.AccessControl.PropagationFlags]::None
    $allow = [Security.AccessControl.AccessControlType]::Allow

    $acl = [Security.AccessControl.DirectorySecurity]::new()
    $acl.SetAccessRuleProtection($true, $false)
    $acl.SetOwner($administrators)
    $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($serviceAccount, $ServiceRights, $inherit, $propagation, $allow))
    $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($systemAccount, [Security.AccessControl.FileSystemRights]::FullControl, $inherit, $propagation, $allow))
    $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($administrators, [Security.AccessControl.FileSystemRights]::FullControl, $inherit, $propagation, $allow))
    Set-Acl -LiteralPath $Path -AclObject $acl
}

function Set-PrivateFileAcl([string]$Path) {
    $serviceAccount = [Security.Principal.NTAccount]::new($serviceIdentity)
    $systemAccount = [Security.Principal.NTAccount]::new('NT AUTHORITY\SYSTEM')
    $administrators = [Security.Principal.NTAccount]::new('BUILTIN\Administrators')
    $allow = [Security.AccessControl.AccessControlType]::Allow
    $acl = [Security.AccessControl.FileSecurity]::new()
    $acl.SetAccessRuleProtection($true, $false)
    $acl.SetOwner($administrators)
    $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($serviceAccount, [Security.AccessControl.FileSystemRights]::FullControl, $allow))
    $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($systemAccount, [Security.AccessControl.FileSystemRights]::FullControl, $allow))
    $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($administrators, [Security.AccessControl.FileSystemRights]::FullControl, $allow))
    Set-Acl -LiteralPath $Path -AclObject $acl
}

function Invoke-Sc([string[]]$Arguments) {
    & "$env:SystemRoot\System32\sc.exe" @Arguments | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Service Control Manager operation failed: $($Arguments[0])."
    }
}

Assert-Administrator
$SourceBinary = Resolve-LocalAbsolutePath $SourceBinary 'SourceBinary'
$InstallRoot = Resolve-LocalAbsolutePath $InstallRoot 'InstallRoot'
$StateRoot = Resolve-LocalAbsolutePath $StateRoot 'StateRoot'
$WorkspaceRoot = Resolve-LocalAbsolutePath $WorkspaceRoot 'WorkspaceRoot'
$managedProgramFiles = [Environment]::GetFolderPath([Environment+SpecialFolder]::ProgramFiles)
$managedProgramData = [Environment]::GetFolderPath([Environment+SpecialFolder]::CommonApplicationData)
$managedInstallRoot = (Join-Path $managedProgramFiles 'Aeontra\Edge').TrimEnd('\')
$managedStateRoot = (Join-Path $managedProgramData 'Aeontra\Edge').TrimEnd('\')
if (-not $InstallRoot.Equals($managedInstallRoot, [StringComparison]::OrdinalIgnoreCase) -or
    -not $StateRoot.Equals($managedStateRoot, [StringComparison]::OrdinalIgnoreCase) -or
    -not $WorkspaceRoot.StartsWith($managedProgramData.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Install/state roots are fixed to Windows known folders and the workspace root must remain under ProgramData.'
}

if ((Test-PathOverlap $InstallRoot $StateRoot) -or (Test-PathOverlap $InstallRoot $WorkspaceRoot) -or (Test-PathOverlap $StateRoot $WorkspaceRoot)) {
    throw 'Install, state and workspace roots must not overlap.'
}
$sourceItem = Get-Item -LiteralPath $SourceBinary -Force
if ($sourceItem.PSIsContainer -or ($sourceItem.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
    throw 'SourceBinary must be a regular non-reparse file.'
}
$sourceBundleRoot = Split-Path -Parent (Split-Path -Parent $SourceBinary)
Assert-NoReparsePath $sourceBundleRoot 'Source bundle path'
$manifestPath = Join-Path $sourceBundleRoot 'manifest.json'
$manifestItem = Get-Item -LiteralPath $manifestPath -Force
if ($manifestItem.PSIsContainer -or ($manifestItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -or $manifestItem.Length -gt 65536) {
    throw 'The Windows bundle manifest is unavailable or unsafe.'
}
foreach ($item in Get-ChildItem -LiteralPath $sourceBundleRoot -Recurse -Force) {
    if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
        throw 'The Windows bundle contains a reparse point.'
    }
}
& $SourceBinary bundle verify | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw 'The signed Windows bundle failed verification.'
}
$manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
if ($manifest.version -ne 6 -or $manifest.platform -ne 'windows' -or $manifest.architecture -ne 'amd64' -or
    $manifest.release -notmatch '^(p15\.[0-9]+\.[0-9]+|v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*))$' -or
    $manifest.commit -notmatch '^[a-f0-9]{40}$') {
    throw 'The signed Windows bundle identity is invalid.'
}
$releaseRoot = Join-Path (Join-Path $InstallRoot 'releases') $manifest.release
$targetBinary = Join-Path $releaseRoot 'bin\mcp-edge.exe'
$activeMarker = Join-Path $InstallRoot 'active.json'

$existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if ($null -ne $existing) {
    throw 'AeontraEdge is already installed. Use the signed bundle updater for upgrades.'
}
foreach ($stalePath in @($serviceConfig, $activeMarker, $pairRequest)) {
    if (Test-Path -LiteralPath $stalePath) {
        throw 'A partial Aeontra Edge installation already exists and requires operator review.'
    }
}

$createdRelease = $false
$createdService = $false
$createdServiceConfig = $false
$createdPairRequest = $false
$createdActiveMarker = $false
$rollbackCanRemoveFiles = $true

try {
New-Item -ItemType Directory -Path $InstallRoot, (Join-Path $InstallRoot 'releases'), $StateRoot, $WorkspaceRoot -Force | Out-Null
Assert-NoReparsePath $InstallRoot 'InstallRoot'
Assert-NoReparsePath (Join-Path $InstallRoot 'releases') 'Release root'
Assert-NoReparsePath $StateRoot 'StateRoot'
Assert-NoReparsePath $WorkspaceRoot 'WorkspaceRoot'

if (Test-Path -LiteralPath $releaseRoot) {
    $existingManifest = Join-Path $releaseRoot 'manifest.json'
    if (-not (Test-Path -LiteralPath $existingManifest -PathType Leaf) -or
        (Get-FileHash -LiteralPath $existingManifest -Algorithm SHA256).Hash -ne (Get-FileHash -LiteralPath $manifestPath -Algorithm SHA256).Hash) {
        throw 'The immutable release directory already exists with another identity.'
    }
} else {
    Copy-Item -LiteralPath $sourceBundleRoot -Destination $releaseRoot -Recurse
    $createdRelease = $true
}
Assert-NoReparsePath $releaseRoot 'Installed release'

& $targetBinary bundle verify | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw 'The installed Windows bundle failed verification.'
}

$quotedBinary = '"' + $targetBinary + '" windows-agent --state "' + $StateRoot + '" --root "' + $WorkspaceRoot + '" --service-identity "' + $serviceIdentity + '" --pair-request "' + $pairRequest + '"'
Invoke-Sc @('create', $serviceName, 'binPath=', $quotedBinary, 'start=', 'auto', 'obj=', $serviceIdentity, 'DisplayName=', 'Aeontra Windows Edge')
$createdService = $true
Invoke-Sc @('sidtype', $serviceName, 'unrestricted')
Invoke-Sc @('failure', $serviceName, 'reset=', '86400', 'actions=', 'restart/5000/restart/15000/restart/60000')
Invoke-Sc @('failureflag', $serviceName, '1')

Set-PrivateDirectoryAcl $InstallRoot ([Security.AccessControl.FileSystemRights]::ReadAndExecute)
Set-PrivateDirectoryAcl $StateRoot ([Security.AccessControl.FileSystemRights]::FullControl)
Set-PrivateDirectoryAcl $WorkspaceRoot ([Security.AccessControl.FileSystemRights]::Modify)

$serviceConfigNext = $serviceConfig + '.next'
$configuration = [ordered]@{
    version = 1
    service = $serviceName
    service_identity = $serviceIdentity
    state_root = $StateRoot
    workspace_root = $WorkspaceRoot
}
[IO.File]::WriteAllText($serviceConfigNext, ($configuration | ConvertTo-Json -Compress), [Text.UTF8Encoding]::new($false))
Move-Item -LiteralPath $serviceConfigNext -Destination $serviceConfig -Force
$createdServiceConfig = $true
Set-PrivateFileAcl $serviceConfig

$identityPath = Join-Path $StateRoot 'identity.json'
if (-not (Test-Path -LiteralPath $identityPath -PathType Leaf)) {
    if ($null -eq $PairingCode) {
        $PairingCode = Read-Host 'Enter the one-time Edge pairing code' -AsSecureString
    }
    $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($PairingCode)
    try {
        $plainCode = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
        if ($plainCode -notmatch '^ep_[A-Za-z0-9._-]{1,125}$') {
            throw 'The pairing code has an invalid shape.'
        }
        $request = [ordered]@{ server = $Server; name = $Name; code = $plainCode }
        [IO.File]::WriteAllText($pairRequest, ($request | ConvertTo-Json -Compress), [Text.UTF8Encoding]::new($false))
        $createdPairRequest = $true
        Set-PrivateFileAcl $pairRequest
    }
    finally {
        if ($pointer -ne [IntPtr]::Zero) {
            [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer)
        }
        $plainCode = $null
    }
}

$activeNext = Join-Path $InstallRoot 'active.json.next'
$active = [ordered]@{ version = 1; release = $manifest.release; commit = $manifest.commit; path = $releaseRoot }
[IO.File]::WriteAllText($activeNext, ($active | ConvertTo-Json -Compress), [Text.UTF8Encoding]::new($false))
Move-Item -LiteralPath $activeNext -Destination $activeMarker -Force
$createdActiveMarker = $true
Set-PrivateFileAcl $activeMarker

Start-Service -Name $serviceName
$service = Get-Service -Name $serviceName
$service.WaitForStatus([ServiceProcess.ServiceControllerStatus]::Running, [TimeSpan]::FromSeconds(30))

$deadline = [DateTime]::UtcNow.AddSeconds(30)
while (-not (Test-Path -LiteralPath $identityPath -PathType Leaf) -and [DateTime]::UtcNow -lt $deadline) {
    Start-Sleep -Milliseconds 500
}
if (-not (Test-Path -LiteralPath $identityPath -PathType Leaf)) {
    throw 'The service started but did not create a paired identity.'
}
if (Test-Path -LiteralPath $pairRequest) {
    throw 'The pairing request was not consumed by the service.'
}

Write-Output "Aeontra Windows Edge installed service=$serviceName state=$StateRoot workspace=$WorkspaceRoot"
}
catch {
    $failure = $_
    $rollbackErrors = [Collections.Generic.List[string]]::new()
    if ($createdService) {
        try {
            $service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
            if ($null -ne $service -and $service.Status -ne [ServiceProcess.ServiceControllerStatus]::Stopped) {
                Stop-Service -Name $serviceName -Force
                $service.WaitForStatus([ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(30))
            }
            & "$env:SystemRoot\System32\sc.exe" delete $serviceName | Out-Null
            if ($LASTEXITCODE -ne 0) { throw 'service deletion failed' }
        }
        catch {
            $rollbackCanRemoveFiles = $false
            $rollbackErrors.Add($_.Exception.Message)
        }
    }
    if ($rollbackCanRemoveFiles) {
        foreach ($candidate in @(
            @{ Created = $createdPairRequest; Path = $pairRequest },
            @{ Created = $createdActiveMarker; Path = $activeMarker },
            @{ Created = $createdServiceConfig; Path = $serviceConfig }
        )) {
            if ($candidate.Created -and (Test-Path -LiteralPath $candidate.Path)) {
                try { Remove-Item -LiteralPath $candidate.Path -Force } catch { $rollbackErrors.Add($_.Exception.Message) }
            }
        }
        if ($createdRelease -and (Test-Path -LiteralPath $releaseRoot)) {
            try {
                Assert-NoReparsePath $releaseRoot 'Rollback release'
                Remove-Item -LiteralPath $releaseRoot -Recurse -Force
            }
            catch { $rollbackErrors.Add($_.Exception.Message) }
        }
    }
    if ($rollbackErrors.Count -ne 0) {
        throw "$($failure.Exception.Message) Installation rollback was incomplete: $($rollbackErrors -join '; ')"
    }
    throw $failure
}
