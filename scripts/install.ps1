# Usage: irm https://raw.githubusercontent.com/pehcastro/tofu/release/scripts/install.ps1 | iex
# Pin a version with $env:TOFU_VERSION, change the place with $env:TOFU_INSTALL_DIR, read assets from $env:TOFU_RELEASE_URL.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$rawArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$arch = switch ($rawArch) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "tofu install: unsupported architecture $rawArch" }
}

$pin = if ($env:TOFU_VERSION) { $env:TOFU_VERSION.TrimStart('v') } else { '' }
$base = if ($env:TOFU_RELEASE_URL) { $env:TOFU_RELEASE_URL.TrimEnd('/') }
    elseif ($pin) { "https://github.com/pehcastro/tofu/releases/download/v$pin" }
    else { 'https://github.com/pehcastro/tofu/releases/latest/download' }
$dir = if ($env:TOFU_INSTALL_DIR) { $env:TOFU_INSTALL_DIR } else { Join-Path $env:USERPROFILE '.local\bin' }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("tofu-install-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $sums = (Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt").Content
    if ($sums -is [byte[]]) { $sums = [Text.Encoding]::UTF8.GetString($sums) }
    $line = $sums -split "`r?`n" | Where-Object { $_ -match "^[0-9a-f]{64}  tofu_.*_windows_$arch\.zip$" } | Select-Object -First 1
    if (-not $line) { throw "tofu install: no windows_$arch asset in $base/checksums.txt" }
    $want, $asset = $line -split '  ', 2
    if ($pin -and $asset -ne "tofu_${pin}_windows_$arch.zip") { throw "tofu install: $base has $asset, not version $pin" }

    Write-Host "downloading $asset"
    $zip = Join-Path $tmp $asset
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $zip
    $got = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash.ToLowerInvariant()
    if ($got -ne $want) { throw "tofu install: checksum mismatch for ${asset}: want $want, got $got" }

    Expand-Archive -Path $zip -DestinationPath $tmp -Force
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $target = Join-Path $dir 'tofu.exe'
    $aside = "$target.old"
    Remove-Item -Force -ErrorAction SilentlyContinue $aside
    if (Test-Path $target) { Move-Item -Force $target $aside }
    Move-Item -Force (Join-Path $tmp 'tofu.exe') $target
    Remove-Item -Force -ErrorAction SilentlyContinue $aside
    Write-Host "installed $target"
    & $target version
} finally {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $tmp
}

$onPath = ($env:PATH -split ';') | Where-Object { $_.TrimEnd('\') -ieq $dir.TrimEnd('\') }
if (-not $onPath) {
    Write-Host "$dir is not on PATH. Add it with:"
    Write-Host "  [Environment]::SetEnvironmentVariable('PATH', [Environment]::GetEnvironmentVariable('PATH', 'User') + ';$dir', 'User')"
}
