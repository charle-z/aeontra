$ErrorActionPreference = 'Stop'
$script = Join-Path $PSScriptRoot 'edge-wsl-availability.ps1'
if (-not (Test-Path -LiteralPath $script)) { throw 'WSL availability installer is missing' }
$plan = & $script -Mode Plan -Distribution parrot -LinuxUser charles | ConvertFrom-Json
if ($plan.executable -ne (Join-Path $env:SystemRoot 'System32\wsl.exe')) { throw 'Unexpected executable' }
if ($plan.arguments -ne '--distribution parrot --user charles --exec /usr/bin/sleep infinity') { throw 'Unexpected arguments' }
if ($plan.task_name -notmatch '^Aeontra-WSL-[a-f0-9]{16}$') { throw 'Unbound task identity' }
$repeat = & $script -Mode Plan -Distribution parrot -LinuxUser charles | ConvertFrom-Json
$other = & $script -Mode Plan -Distribution other -LinuxUser charles | ConvertFrom-Json
if ($repeat.task_name -ne $plan.task_name -or $other.task_name -eq $plan.task_name) { throw 'Task binding failed' }
foreach ($invalid in @('../parrot', 'parrot" --exec cmd', 'parrot;cmd', 'parrot`nother')) {
    $rejected = $false
    try { & $script -Mode Plan -Distribution $invalid -LinuxUser charles | Out-Null } catch { $rejected = $true }
    if (-not $rejected) { throw 'Unsafe distribution accepted' }
}
'WSL_AVAILABILITY_PLAN=PASS'
