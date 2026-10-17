$ErrorActionPreference = "Stop"
$desk = Split-Path $PSScriptRoot -Parent
$out = Join-Path $desk "target_release\motion"
$bin = Join-Path $out "bin"
$env:TOFU_DESKTOP_CHANNEL = "release"
try {
  cargo build -j 4 --profile motion -p desk -p tofu-desk --features desk/all-screens --manifest-path (Join-Path $desk "Cargo.toml") --target-dir (Join-Path $desk "target_release")
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
  Remove-Item Env:TOFU_DESKTOP_CHANNEL
}
New-Item -ItemType Directory -Force -Path $bin | Out-Null
Move-Item -Force -LiteralPath (Join-Path $out "tofu-desk.exe") -Destination (Join-Path $bin "tofu-desk.exe")
Remove-Item -Force -ErrorAction SilentlyContinue -LiteralPath (Join-Path $out "desk.exe"), (Join-Path $out "desk.pdb")
Join-Path $out "tofu-desktop.exe"
Join-Path $bin "tofu-desk.exe"
