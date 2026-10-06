param([switch]$Quiet)
$ErrorActionPreference = "Stop"
$stale = (Get-Date).AddDays(-3)
$target = Join-Path (Split-Path $PSScriptRoot -Parent) "target"
if (-not (Test-Path $target)) { "target absent"; exit 0 }
$keepDirs = @("debug", "release", "motion", "profiling", "walls")
$keepFiles = @(".rustc_info.json", "CACHEDIR.TAG")
$loose = @("*.log", "*.out", "*.png", "*.err")

function Size-GB {
  $sum = (Get-ChildItem -LiteralPath $target -Recurse -File -Force -ErrorAction SilentlyContinue | Measure-Object Length -Sum).Sum
  [math]::Round(($sum / 1GB), 1)
}

function Remove-Quiet($path) {
  Remove-Item -LiteralPath $path -Recurse -Force -ErrorAction SilentlyContinue
  if (Test-Path -LiteralPath $path) { Write-Warning "in use, kept: $path" }
}

if (-not $Quiet) { $before = Size-GB }
foreach ($dir in Get-ChildItem -LiteralPath $target -Directory -Force) {
  if ($keepDirs -notcontains $dir.Name) { Remove-Quiet $dir.FullName }
}
$walls = Join-Path $target "walls"
foreach ($file in Get-ChildItem -LiteralPath $target -File -Force) {
  if ($keepFiles -contains $file.Name) { continue }
  if ($file.Name -like "*.wall") {
    New-Item -ItemType Directory -Force -Path $walls | Out-Null
    Move-Item -LiteralPath $file.FullName -Destination (Join-Path $walls $file.Name) -Force
    continue
  }
  foreach ($pattern in $loose) {
    if ($file.Name -like $pattern) { Remove-Quiet $file.FullName; break }
  }
}
foreach ($profile in Get-ChildItem -LiteralPath $target -Directory -Force) {
  $incremental = Join-Path $profile.FullName "incremental"
  if (-not (Test-Path $incremental)) { continue }
  Get-ChildItem -LiteralPath $incremental -Directory -Force | Where-Object { $_.LastWriteTime -lt $stale } | ForEach-Object { Remove-Quiet $_.FullName }
}
if ($Quiet) { "pruned" } else { "target $before GB -> $(Size-GB) GB" }
