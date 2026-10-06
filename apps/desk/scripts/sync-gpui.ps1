param([Parameter(Mandatory = $true)][string]$Rev)
$ErrorActionPreference = "Stop"
if ($Rev -notmatch '^[0-9a-f]{40}$') { throw "Rev must be a full 40 character commit sha, got '$Rev'" }
$desk = Split-Path -Parent $PSScriptRoot
$vendor = Join-Path $desk "vendor"
$target = Join-Path $vendor "gpui-ce"
$staging = Join-Path $vendor "gpui-ce.sync"
if (Test-Path $staging) { Remove-Item -Recurse -Force $staging }
New-Item -ItemType Directory $staging | Out-Null
function Git([string[]]$GitArgs) {
  & git -C $staging @GitArgs
  if ($LASTEXITCODE -ne 0) { throw "git $($GitArgs -join ' ') failed with $LASTEXITCODE" }
}
Git @("init", "-q")
Git @("remote", "add", "origin", "https://github.com/gpui-ce/gpui-ce")
Git @("fetch", "-q", "--depth", "1", "origin", $Rev)
Git @("checkout", "-q", "FETCH_HEAD")
$fetched = (& git -C $staging rev-parse HEAD).Trim()
if ($fetched -ne $Rev) { throw "fetched $fetched, wanted $Rev" }
Remove-Item -Recurse -Force (Join-Path $staging ".git")
$goFiles = Get-ChildItem -Path $staging -Recurse -Filter *.go -File
if ($goFiles) { throw "the vendored tree holds .go files, which the Go build would see: $($goFiles.FullName -join ', ')" }
if (Test-Path $target) { Remove-Item -Recurse -Force $target }
Move-Item $staging $target
Copy-Item (Join-Path $target "Cargo.lock") (Join-Path $desk "Cargo.lock") -Force
"vendored gpui-ce at $Rev into $target; update vendor/UPSTREAM.md in the same commit"
