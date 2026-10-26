[CmdletBinding()]
param(
    [ValidateSet('Plan', 'Install', 'Status', 'Remove', 'Run')][string]$Mode = 'Plan',
    [Parameter(Mandatory)][ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$')][string]$Distribution,
    [Parameter(Mandatory)][ValidatePattern('^[a-z_][a-z0-9_-]{0,31}$')][string]$LinuxUser,
    [string]$Root
)
$ErrorActionPreference = 'Stop'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$binding = "$identity/$Distribution/$LinuxUser"
$sha = [Security.Cryptography.SHA256]::Create()
try { $hash = ([BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($binding)))).Replace('-', '').ToLowerInvariant() }
finally { $sha.Dispose() }
$taskName = 'Aeontra-WSL-' + $hash.Substring(0, 16)
$description = "Aeontra WSL availability v1: $binding"
$wsl = Join-Path $env:SystemRoot 'System32\wsl.exe'
$arguments = "--distribution $Distribution --user $LinuxUser --exec /usr/bin/sleep infinity"
if ($Mode -eq 'Plan') {
    [pscustomobject]@{ task_name = $taskName; executable = $wsl; arguments = $arguments; principal = $identity } | ConvertTo-Json -Compress
    exit 0
}
if ($Mode -eq 'Run') {
    # One Windows-owned process keeps the distro alive. systemd still owns the Edge.
    & $wsl --distribution $Distribution --user $LinuxUser --exec /usr/bin/sleep infinity
    exit $LASTEXITCODE
}
$existing = Get-ScheduledTask -TaskName $taskName -TaskPath '\' -ErrorAction SilentlyContinue
$existingSid = $null
if ($existing) {
    # Task Scheduler may return the account name even when registered by SID.
    $account = $existing.Principal.UserId
    if ($account.StartsWith('S-1-')) { $existingSid = ([Security.Principal.SecurityIdentifier]::new($account)).Value }
    else { $existingSid = ([Security.Principal.NTAccount]::new($account)).Translate([Security.Principal.SecurityIdentifier]).Value }
}
if ($existing -and ($existing.Description -ne $description -or $existingSid -ne $identity)) {
    throw 'Existing task ownership does not match; it was not changed'
}
if ($Mode -eq 'Status') {
    if (-not $existing) { [pscustomobject]@{ task_name = $taskName; state = 'absent' } | ConvertTo-Json -Compress; exit 0 }
    $info = Get-ScheduledTaskInfo -TaskName $taskName -TaskPath '\'
    [pscustomobject]@{ task_name = $taskName; state = [string]$existing.State; last_result = $info.LastTaskResult } | ConvertTo-Json -Compress
    exit 0
}
if ($Mode -eq 'Remove') {
    if ($existing) { Stop-ScheduledTask -TaskName $taskName -TaskPath '\'; Unregister-ScheduledTask -TaskName $taskName -TaskPath '\' -Confirm:$false }
    # Preserve the installed script and never terminate the distro or Edge service.
    [pscustomobject]@{ task_name = $taskName; state = 'removed' } | ConvertTo-Json -Compress
    exit 0
}
if (-not $Root -or -not [IO.Path]::IsPathRooted($Root) -or $Root.StartsWith('\\') -or $Root.IndexOfAny([char[]]"`"`r`n") -ge 0) { throw 'Select an absolute local installation directory' }
$Root = [IO.Path]::GetFullPath($Root).TrimEnd('\')
if ($Root -eq [IO.Path]::GetPathRoot($Root).TrimEnd('\') -or $Root.StartsWith($env:SystemRoot, [StringComparison]::OrdinalIgnoreCase)) { throw 'Do not install in a drive root or Windows directory' }
$check = $Root
while ($check) {
    if (Test-Path -LiteralPath $check) {
        $item = Get-Item -LiteralPath $check -Force
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Installation path contains a reparse point' }
    }
    $parent = [IO.Path]::GetDirectoryName($check)
    if ($parent -eq $check) { break }
    $check = $parent
}
& $wsl --distribution $Distribution --user $LinuxUser --exec mcp-edge doctor
if ($LASTEXITCODE -ne 0) { throw 'Edge doctor failed; task was not installed' }
$installed = Join-Path $Root 'edge-wsl-availability.ps1'
$powershell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
$taskArguments = "-NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$installed`" -Mode Run -Distribution $Distribution -LinuxUser $LinuxUser"
if ($existing) {
    if ($existing.Actions.Count -ne 1 -or $existing.Actions[0].Execute -ne $powershell -or $existing.Actions[0].Arguments -ne $taskArguments -or
        -not (Test-Path -LiteralPath $installed) -or (Get-FileHash -LiteralPath $installed).Hash -ne (Get-FileHash -LiteralPath $PSCommandPath).Hash) {
        throw 'Existing task definition differs; remove the exact owned task before reinstalling'
    }
} else {
    if ((Test-Path -LiteralPath $installed) -and (Get-FileHash -LiteralPath $installed).Hash -ne (Get-FileHash -LiteralPath $PSCommandPath).Hash) { throw 'Existing installation file differs; it was not overwritten' }
    New-Item -ItemType Directory -Path $Root -Force | Out-Null
    if ([IO.Path]::GetFullPath($PSCommandPath) -ne $installed) { Copy-Item -LiteralPath $PSCommandPath -Destination $installed }
    $action = New-ScheduledTaskAction -Execute $powershell -Argument $taskArguments
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity
    $principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
    $settings = New-ScheduledTaskSettingsSet -Hidden -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
    Register-ScheduledTask -TaskName $taskName -TaskPath '\' -Description $description -Action $action -Trigger $trigger -Principal $principal -Settings $settings | Out-Null
}
if (-not $existing -or $existing.State -ne 'Running') { Start-ScheduledTask -TaskName $taskName -TaskPath '\' }
[pscustomobject]@{ task_name = $taskName; state = 'installed'; distribution = $Distribution; linux_user = $LinuxUser } | ConvertTo-Json -Compress
