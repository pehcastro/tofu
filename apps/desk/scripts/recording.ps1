$ErrorActionPreference = "Stop"
$recordings = Join-Path (Split-Path $PSScriptRoot -Parent) "target\recordings"
$newest = Get-ChildItem -LiteralPath $recordings -Directory -ErrorAction SilentlyContinue | Sort-Object Name | Select-Object -Last 1
if (-not $newest) { "no recording under $recordings"; exit 1 }
$summary = Join-Path $newest.FullName "summary.txt"
if (-not (Test-Path $summary)) { "$($newest.FullName) has no summary.txt yet, the writer may still be running"; exit 1 }
$newest.FullName
Invoke-Item $newest.FullName
Get-Content -Raw $summary
