[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [switch]$RemoveData,
    [string]$InstallRoot = (Join-Path ([Environment]::GetFolderPath([Environment+SpecialFolder]::ProgramFiles)) 'Aeontra\Edge'),
    [string]$StateRoot = (Join-Path ([Environment]::GetFolderPath([Environment+SpecialFolder]::CommonApplicationData)) 'Aeontra\Edge'),
    [string]$WorkspaceRoot = (Join-Path ([Environment]::GetFolderPath([Environment+SpecialFolder]::CommonApplicationData)) 'Aeontra\Workspaces')
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Aeontra Edge removal requires an elevated PowerShell session.'
}

function Resolve-ManagedRoot([string]$Path, [string]$ExpectedParent, [string]$Label) {
    $full = [IO.Path]::GetFullPath($Path).TrimEnd('\')
    $parent = [IO.Path]::GetFullPath($ExpectedParent).TrimEnd('\')
    $volume = [IO.Path]::GetPathRoot($full).TrimEnd('\')
    if ([string]::IsNullOrWhiteSpace($full) -or $full -eq $volume -or
        (-not $full.StartsWith($parent + '\', [StringComparison]::OrdinalIgnoreCase))) {
        throw "$Label is outside its managed installation parent."
    }
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
    return $full
}

function Resolve-ManagedInstallRoot([string]$Path) {
    $full = [IO.Path]::GetFullPath($Path).TrimEnd('\')
    $root = [IO.Path]::GetPathRoot($full)
    if ([string]::IsNullOrWhiteSpace($root) -or $root.StartsWith('\') -or $full.StartsWith('\\?\') -or $full.StartsWith('\\.\')) {
        throw 'InstallRoot must be an absolute path on a local drive.'
    }
    $volume = [IO.DriveInfo]::new($root)
    if (-not $volume.IsReady -or $volume.DriveType -ne [IO.DriveType]::Fixed) {
        throw 'InstallRoot must use a ready fixed local drive.'
    }
    if (-not (Split-Path -Leaf $full).Equals('Edge', [StringComparison]::OrdinalIgnoreCase) -or
        -not (Split-Path -Leaf (Split-Path -Parent $full)).Equals('Aeontra', [StringComparison]::OrdinalIgnoreCase)) {
        throw 'InstallRoot must end in Aeontra\Edge.'
    }
    return $full
}

function Assert-NoReparseTree([string]$Path, [string]$Label) {
    if (-not (Test-Path -LiteralPath $Path)) { return }
    $root = Get-Item -LiteralPath $Path -Force
    if ($root.Attributes -band [IO.FileAttributes]::ReparsePoint) {
        throw "$Label is a reparse point."
    }
    foreach ($item in Get-ChildItem -LiteralPath $Path -Recurse -Force) {
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
            throw "$Label contains a reparse point."
        }
    }
}

$programData = [Environment]::GetFolderPath([Environment+SpecialFolder]::CommonApplicationData)
$InstallRoot = Resolve-ManagedInstallRoot $InstallRoot
$StateRoot = Resolve-ManagedRoot $StateRoot $programData 'StateRoot'
$WorkspaceRoot = Resolve-ManagedRoot $WorkspaceRoot $programData 'WorkspaceRoot'

$service = Get-Service -Name 'AeontraEdge' -ErrorAction SilentlyContinue
if ($null -ne $service -and $PSCmdlet.ShouldProcess('AeontraEdge', 'stop and delete Windows service')) {
    if ($service.Status -ne [ServiceProcess.ServiceControllerStatus]::Stopped) {
        Stop-Service -Name 'AeontraEdge' -Force
        $service.WaitForStatus([ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(30))
    }
    & "$env:SystemRoot\System32\sc.exe" delete AeontraEdge | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw 'Windows service deletion failed.'
    }
}

Assert-NoReparseTree $InstallRoot 'InstallRoot'
if ((Test-Path -LiteralPath $InstallRoot) -and $PSCmdlet.ShouldProcess($InstallRoot, 'remove versioned Aeontra Edge program files')) {
    Remove-Item -LiteralPath $InstallRoot -Recurse -Force
}

if ($RemoveData) {
    if ($PSCmdlet.ShouldProcess($StateRoot, 'remove paired identity and private Edge state')) {
        Assert-NoReparseTree $StateRoot 'StateRoot'
        Remove-Item -LiteralPath $StateRoot -Recurse -Force
    }
    if ($PSCmdlet.ShouldProcess($WorkspaceRoot, 'remove registered Windows workspaces')) {
        Assert-NoReparseTree $WorkspaceRoot 'WorkspaceRoot'
        Remove-Item -LiteralPath $WorkspaceRoot -Recurse -Force
    }
} else {
    Write-Output 'State and workspaces were preserved. Use -RemoveData only for an intentional destructive uninstall.'
}
