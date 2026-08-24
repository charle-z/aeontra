[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Root,

    [Parameter(Mandatory = $true)]
    [string]$Output
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$Root = [IO.Path]::GetFullPath($Root).TrimEnd('\')
$Output = [IO.Path]::GetFullPath($Output)
if (-not (Test-Path -LiteralPath $Root -PathType Container) -or (Test-Path -LiteralPath $Output)) {
    throw 'Archive input must be an existing directory and output must be new.'
}
if ($Output.StartsWith($Root + '\', [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Archive output must be outside the input tree.'
}

$files = @(Get-ChildItem -LiteralPath $Root -File -Recurse -Force | Sort-Object { [IO.Path]::GetRelativePath($Root, $_.FullName).Replace('\', '/') })
if ($files.Count -eq 0) {
    throw 'Archive input is empty.'
}
foreach ($item in Get-ChildItem -LiteralPath $Root -Recurse -Force) {
    if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
        throw 'Archive input contains a reparse point.'
    }
}

$parent = Split-Path -Parent $Output
New-Item -ItemType Directory -Path $parent -Force | Out-Null
$stream = $null
$archive = $null
try {
    $stream = [IO.File]::Open($Output, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    $archive = [IO.Compression.ZipArchive]::new($stream, [IO.Compression.ZipArchiveMode]::Create, $false)
    $timestamp = [DateTimeOffset]::new(1980, 1, 1, 0, 0, 0, [TimeSpan]::Zero)
    foreach ($file in $files) {
        $relative = [IO.Path]::GetRelativePath($Root, $file.FullName).Replace('\', '/')
        $entry = $archive.CreateEntry($relative, [IO.Compression.CompressionLevel]::NoCompression)
        $entry.LastWriteTime = $timestamp
        $input = [IO.File]::OpenRead($file.FullName)
        $outputStream = $entry.Open()
        try { $input.CopyTo($outputStream) }
        finally {
            $outputStream.Dispose()
            $input.Dispose()
        }
    }
}
catch {
    if ($null -ne $archive) { $archive.Dispose(); $archive = $null }
    if ($null -ne $stream) { $stream.Dispose(); $stream = $null }
    Remove-Item -LiteralPath $Output -Force -ErrorAction SilentlyContinue
    throw
}
finally {
    if ($null -ne $archive) { $archive.Dispose() }
    if ($null -ne $stream) { $stream.Dispose() }
}
