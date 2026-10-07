param(
  [string]$Page = "",
  [string]$Theme = "",
  [ValidateSet("", "light", "dark")][string]$Mode = ""
)
$ErrorActionPreference = "Stop"
$desk = Split-Path $PSScriptRoot -Parent
$log = Join-Path $desk "target\feel.log"
New-Item -ItemType Directory -Force -Path (Join-Path $desk "target") | Out-Null
Push-Location $desk
try {
  & (Join-Path $PSScriptRoot "cargo-low.ps1") -Log $log -Cargo "build -p desk_book --profile motion -j 4 --message-format short"
  $built = $LASTEXITCODE
} finally {
  Pop-Location
}
if ($built -ne 0) {
  Get-Content $log | Where-Object { $_ -match "^(error|warning)|^crates" } | Select-Object -First 40
  exit $built
}
$arguments = @()
if ($Page) { $arguments += @("--page", $Page) }
if ($Theme) { $arguments += @("--theme", $Theme) }
if ($Mode) { $arguments += @("--mode", $Mode) }
$exe = Join-Path $desk "target\motion\book.exe"
if ($arguments.Count -gt 0) {
  Start-Process -FilePath $exe -ArgumentList $arguments -WorkingDirectory $desk
} else {
  Start-Process -FilePath $exe -WorkingDirectory $desk
}
