param(
    [Parameter(Mandatory = $true)][string]$Plugin,
    [Parameter(Mandatory = $true)][string]$Out,
    [string]$TargetDir = (Join-Path $PSScriptRoot '..\target')
)

$ErrorActionPreference = 'Stop'

function Read-Key([string]$Path, [string]$Key) {
    $line = Select-String -Path $Path -Pattern "^\s*$Key\s*=\s*`"([^`"]+)`"" | Select-Object -First 1
    if (-not $line) { throw "$Path has no $Key" }
    $line.Matches[0].Groups[1].Value
}

function Invoke-Cargo([string[]]$Arguments) {
    & cargo @Arguments
    if ($LASTEXITCODE -ne 0) { throw "cargo $($Arguments -join ' ') exited $LASTEXITCODE" }
}

$Plugin = (Resolve-Path $Plugin).Path
$manifest = Join-Path $Plugin 'plugin.toml'
$id = Read-Key $manifest 'id'
$crate = (Read-Key (Join-Path $Plugin 'Cargo.toml') 'name') -replace '-', '_'
$sdk = Join-Path $PSScriptRoot '..\crates\plugin-sdk\Cargo.toml'
$core = Join-Path $TargetDir "wasm32-unknown-unknown\release\$crate.wasm"
$folder = Join-Path $Out $id
$wasm = Join-Path $folder 'plugin.wasm'

$wall = [Diagnostics.Stopwatch]::StartNew()
Invoke-Cargo @('build', '-j', '4', '--release', '--target', 'wasm32-unknown-unknown', '--manifest-path', (Join-Path $Plugin 'Cargo.toml'), '--target-dir', $TargetDir)
$built = $wall.Elapsed.TotalMilliseconds
New-Item -ItemType Directory -Force -Path $folder | Out-Null
Invoke-Cargo @('run', '-q', '-j', '4', '--manifest-path', $sdk, '--example', 'pack', '--target-dir', $TargetDir, '--', $core, $wasm)
Copy-Item -Force $manifest (Join-Path $folder 'plugin.toml')
$wall.Stop()

$bytes = (Get-Item $wasm).Length
$total = $wall.Elapsed.TotalMilliseconds
'pack {0}: {1:N0} ms wall (wasm32 build {2:N0} ms, component {3:N0} ms), plugin.wasm {4} bytes' -f $id, $total, $built, ($total - $built), $bytes
"folder: $folder"
