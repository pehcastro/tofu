param(
  [string]$Exe = "",
  [switch]$SkipMenu,
  [switch]$SkipTooltip,
  [double]$Threshold = 0,
  [ValidateSet("Glass", "Flat")][string]$Theme = "Glass"
)
$ErrorActionPreference = "Stop"
$desk = Split-Path $PSScriptRoot -Parent
if (-not $Exe) { $Exe = Join-Path $desk "target_flows\motion\tofu-desktop.exe" }
if (-not (Test-Path $Exe)) { "no desk at $Exe"; exit 2 }
$Exe = (Resolve-Path $Exe).Path
$margin = 3.0
$curveClearance = 1
$edgeOut = 2
$windowInset = 4
$timeout = [TimeSpan]::FromSeconds(60)
$menuTimeout = [TimeSpan]::FromSeconds(5)
$settle = 1500
$menuSettle = 400
$hoverSettle = 400
$windowColour = "#ffffffff"
$themeBase = @{ Glass = "tofu-glass"; Flat = "tofu-flat" }[$Theme]
$themeFile = "corner-$($Theme.ToLower())"
$themeMode = "dark"

function Read-Const([string]$file, [string]$name) {
  $text = Get-Content -Raw -LiteralPath (Join-Path $desk $file)
  if ($text -notmatch "const $name\s*:\s*f32\s*=\s*([0-9.]+)\s*;") { throw "no const $name in $file" }
  [double]$Matches[1]
}
$sidebarRight = (Read-Const "crates\ui\src\metrics.rs" "SIDEBAR_WIDTH") + 2 * (Read-Const "crates\ui\src\components\sidebar.rs" "SIDEBAR_PAD_X")
$groupPad = Read-Const "crates\ui\src\components\size.rs" "GROUP_PAD_BOTTOM"
$lineCap = Read-Const "crates\ui\src\components\size.rs" "LINE_CAP"
$addButton = Read-Const "crates\ui\src\components\sidebar.rs" "NEW_SESSION"
$iconSmall = Read-Const "crates\ui\src\metrics.rs" "ICON_SMALL"
$titleHeight = Read-Const "crates\ui\src\metrics.rs" "TITLE_BAR_HEIGHT"
$control = Read-Const "crates\ui\src\metrics.rs" "CONTROL"

Add-Type -AssemblyName System.Drawing
Add-Type @"
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
public struct DeskCornerRect { public int Left, Top, Right, Bottom; }
public class DeskCornerCheck {
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr h, out DeskCornerRect r);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr dc, uint flags);
  [DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr h);
  [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)] static extern IntPtr CreateDesktopW(string name, IntPtr device, IntPtr mode, uint flags, uint access, IntPtr attrs);
  [DllImport("user32.dll")] public static extern bool CloseDesktop(IntPtr d);
  delegate bool EnumProc(IntPtr h, IntPtr l);
  [DllImport("user32.dll")] static extern bool EnumDesktopWindows(IntPtr d, EnumProc f, IntPtr l);
  [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll")] static extern IntPtr GetWindow(IntPtr h, uint cmd);
  [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
  [DllImport("kernel32.dll", SetLastError = true)] static extern bool SetHandleInformation(IntPtr h, uint mask, uint flags);
  [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr h);
  [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)] static extern bool CreateProcessW(string app, System.Text.StringBuilder line, IntPtr pa, IntPtr ta, bool inherit, uint flags, IntPtr env, string dir, ref STARTUPINFO si, out PROCESS_INFORMATION pi);
  [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)] struct STARTUPINFO { public int cb; public string reserved, desktop, title; public int x, y, xs, ys, xc, yc, fill, flags; public short show, r2; public IntPtr r3, stdin, stdout, stderr; }
  [StructLayout(LayoutKind.Sequential)] struct PROCESS_INFORMATION { public IntPtr process, thread; public int pid, tid; }
  public static IntPtr NewDesktop(string name) {
    IntPtr d = CreateDesktopW(name, IntPtr.Zero, IntPtr.Zero, 0, 0x01FF, IntPtr.Zero);
    if (d == IntPtr.Zero) throw new Win32Exception();
    return d;
  }
  public static int Start(string desktop, string commandLine, string dir, IntPtr log) {
    SetHandleInformation(log, 1, 1);
    STARTUPINFO si = new STARTUPINFO();
    si.cb = Marshal.SizeOf(si); si.desktop = desktop; si.flags = 0x100; si.stdout = log; si.stderr = log;
    PROCESS_INFORMATION pi;
    if (!CreateProcessW(null, new System.Text.StringBuilder(commandLine), IntPtr.Zero, IntPtr.Zero, true, 0, IntPtr.Zero, dir, ref si, out pi)) throw new Win32Exception();
    CloseHandle(pi.thread); CloseHandle(pi.process);
    return pi.pid;
  }
  public static IntPtr FindWindow(IntPtr desktop, int pid) {
    IntPtr found = IntPtr.Zero;
    EnumDesktopWindows(desktop, (h, l) => {
      uint owner; GetWindowThreadProcessId(h, out owner);
      if (owner == pid && GetWindow(h, 4) == IntPtr.Zero && IsWindowVisible(h)) { found = h; return false; }
      return true;
    }, IntPtr.Zero);
    return found;
  }
  public static IntPtr Point(int x, int y) { return (IntPtr)((y << 16) | (x & 0xFFFF)); }
}
"@
$WM_CLOSE = 0x0010; $WM_KEYDOWN = 0x0100; $WM_MOUSEMOVE = 0x0200; $WM_LBUTTONDOWN = 0x0201; $WM_LBUTTONUP = 0x0202

$root = Join-Path ([IO.Path]::GetTempPath()) ("desk-corner-check-" + [guid]::NewGuid().ToString("N"))
$userHome = Join-Path $root "home"
$deskHome = Join-Path $userHome ".tofu\desk"
foreach ($folder in @("config", "themes")) { New-Item -ItemType Directory -Force -Path (Join-Path $deskHome $folder) | Out-Null }
$themeJson = [ordered]@{
  name = "Corner $Theme on white"
  extends = $themeBase
  glass = @{ tint = 1 }
  dark = @{ surface = @{ window = $windowColour } }
  light = @{ surface = @{ window = $windowColour } }
} | ConvertTo-Json -Depth 4
[IO.File]::WriteAllText((Join-Path $deskHome "themes\$themeFile.json"), $themeJson)
[IO.File]::WriteAllText((Join-Path $deskHome "config\theme"), "$themeFile $themeMode`n")
$repos = @(foreach ($name in @("alpha", "beta")) {
  $repo = Join-Path $root $name
  New-Item -ItemType Directory -Force -Path $repo | Out-Null
  Set-Content -LiteralPath (Join-Path $repo "README.md") -Value $name
  git -C $repo init -q -b main | Out-Null
  git -C $repo add README.md | Out-Null
  git -C $repo -c user.name=corner -c user.email=corner@localhost commit -q -m init | Out-Null
  if ($LASTEXITCODE -ne 0) { "could not make the git repo $repo"; exit 2 }
  $repo
})
$errLog = Join-Path $root "desk.err.log"

function Read-Log {
  if (-not (Test-Path $errLog)) { return "" }
  $stream = [IO.File]::Open($errLog, "Open", "Read", "ReadWrite")
  try { (New-Object IO.StreamReader($stream)).ReadToEnd() } finally { $stream.Dispose() }
}

function Read-Probes {
  $found = @{ layout = @{}; corners = @() }
  foreach ($line in ((Read-Log) -split "`r?`n")) {
    if ($line -match "^layout (\S+) edge=(left|right) x=(-?[0-9.]+)") {
      if (-not $found.layout.ContainsKey($Matches[1])) { $found.layout[$Matches[1]] = [double]$Matches[3] }
    }
    if ($line -match "^corner (\w+)@\S+ x=(-?[0-9.]+) y=(-?[0-9.]+) w=([0-9.]+) h=([0-9.]+) r=([0-9.]+)") {
      $found.corners += [pscustomobject]@{ kind = $Matches[1]; x = [double]$Matches[2]; y = [double]$Matches[3]; w = [double]$Matches[4]; h = [double]$Matches[5]; r = [double]$Matches[6] }
    }
  }
  $found
}

function Current-Corners($logged) {
  $kept = @()
  for ($i = $logged.Count - 1; $i -ge 0; $i--) {
    $c = $logged[$i]
    if ($c.w -le 0 -or $c.h -le 0) { continue }
    $covered = @($kept | Where-Object { $_.kind -eq $c.kind -and $_.x -lt ($c.x + $c.w) -and $c.x -lt ($_.x + $_.w) -and $_.y -lt ($c.y + $c.h) -and $c.y -lt ($_.y + $_.h) })
    if ($covered.Count -eq 0) { $kept += $c }
  }
  $kept
}

function Sidebar-Cards($current) { @($current | Where-Object { $_.kind -eq "card" -and $_.x -lt $sidebarRight } | Sort-Object y) }
function Work-Tiles($current) { @($current | Where-Object { $_.kind -eq "card" -and $_.x -ge $sidebarRight } | Sort-Object y, x) }

function Descendants([int]$parent) {
  $all = Get-CimInstance Win32_Process -Property ProcessId, ParentProcessId
  $pending = @($parent); $found = @()
  while ($pending.Count -gt 0) {
    $next = @()
    foreach ($id in $pending) {
      foreach ($child in ($all | Where-Object { $_.ParentProcessId -eq $id })) { $found += [int]$child.ProcessId; $next += [int]$child.ProcessId }
    }
    $pending = $next
  }
  $found
}

function Capture($handle) {
  $rect = New-Object DeskCornerRect
  [DeskCornerCheck]::GetClientRect($handle, [ref]$rect) | Out-Null
  $bitmap = New-Object System.Drawing.Bitmap ([Math]::Max(1, $rect.Right)), ([Math]::Max(1, $rect.Bottom))
  $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
  $dc = $graphics.GetHdc()
  [DeskCornerCheck]::PrintWindow($handle, $dc, 3) | Out-Null
  $graphics.ReleaseHdc($dc)
  $graphics.Dispose()
  $bitmap
}

function Device([double]$v) { [int][Math]::Round($v * $script:scale) }

function Luma($bitmap, [int]$px, [int]$py) {
  if ($px -lt 0 -or $py -lt 0 -or $px -ge $bitmap.Width -or $py -ge $bitmap.Height) { return $null }
  $c = $bitmap.GetPixel($px, $py)
  0.2126 * $c.R + 0.7152 * $c.G + 0.0722 * $c.B
}

function Inward([int]$edge, [int]$side, [int]$by) { if ($side -gt 0) { $edge + $by } else { $edge - 1 - $by } }
function Outward([int]$edge, [int]$side, [int]$by) { Inward $edge (-$side) $by }

function Gap-Offset([double]$radius) {
  [int][Math]::Max(1, [Math]::Floor((Device $radius) * (1 - 1 / [Math]::Sqrt(2)) - $curveClearance))
}

function Corner-Pair($bitmap, [int]$cx, [int]$cy, [int]$sx, [int]$sy, [int]$by) {
  $gap = Luma $bitmap (Inward $cx $sx $by) (Inward $cy $sy $by)
  $out = Luma $bitmap (Outward $cx $sx $by) (Outward $cy $sy $by)
  if ($null -eq $gap -or $null -eq $out) { return $null }
  [pscustomobject]@{ gap = $gap; out = $out; diff = [Math]::Abs($gap - $out) }
}

function Click($handle, [double]$x, [double]$y) {
  $at = [DeskCornerCheck]::Point((Device $x), (Device $y))
  [DeskCornerCheck]::PostMessage($handle, $WM_MOUSEMOVE, [IntPtr]0, $at) | Out-Null
  [DeskCornerCheck]::PostMessage($handle, $WM_LBUTTONDOWN, [IntPtr]1, $at) | Out-Null
  [DeskCornerCheck]::PostMessage($handle, $WM_LBUTTONUP, [IntPtr]0, $at) | Out-Null
}

function Wait-Quiet([scriptblock]$ready, [TimeSpan]$within, [int]$quietMs) {
  $deadline = (Get-Date) + $within
  $seen = -1
  $quietSince = Get-Date
  while ((Get-Date) -lt $deadline -and -not $proc.HasExited) {
    $probes = Read-Probes
    if ($probes.corners.Count -ne $seen) { $seen = $probes.corners.Count; $quietSince = Get-Date }
    if ((& $ready $probes) -and ((Get-Date) - $quietSince).TotalMilliseconds -ge $quietMs) { return $true }
    Start-Sleep -Milliseconds 100
  }
  $false
}

$saved = @{ USERPROFILE = $env:USERPROFILE; APPDATA = $env:APPDATA; DESK_LAYOUT_LOG = $env:DESK_LAYOUT_LOG; DESK_FIRST_RUN = $env:DESK_FIRST_RUN; TOFU_DESK_THEME = $env:TOFU_DESK_THEME }
Remove-Item env:TOFU_DESK_THEME -ErrorAction SilentlyContinue
$env:USERPROFILE = $userHome
$env:APPDATA = Join-Path $userHome "AppData\Roaming"
$env:DESK_LAYOUT_LOG = "1"
$env:DESK_FIRST_RUN = "ready"
$desktopName = "desk-corner-check-" + [guid]::NewGuid().ToString("N")
try {
  $desktop = [DeskCornerCheck]::NewDesktop($desktopName)
} catch { "could not make a hidden desktop: $($_.Exception.Message)"; exit 2 }
$logStream = [IO.File]::Open($errLog, "Create", "Write", "ReadWrite")
try {
  $line = "`"$Exe`" " + (($repos | ForEach-Object { "`"$_`"" }) -join " ")
  $deskPid = [DeskCornerCheck]::Start($desktopName, $line, $desk, $logStream.SafeFileHandle.DangerousGetHandle())
  $proc = Get-Process -Id $deskPid
} catch {
  "could not start the desk on the hidden desktop: $($_.Exception.Message)"; [DeskCornerCheck]::CloseDesktop($desktop) | Out-Null; exit 2
} finally {
  $logStream.Dispose()
  foreach ($name in $saved.Keys) { Set-Item -Path "env:$name" -Value $saved[$name] -ErrorAction SilentlyContinue; if ($null -eq $saved[$name]) { Remove-Item "env:$name" -ErrorAction SilentlyContinue } }
}

$shots = [ordered]@{}
$children = @()
$handle = [IntPtr]::Zero
$script:scale = 1.0
$notes = @()
try {
  $deadline = (Get-Date) + $timeout
  while ((Get-Date) -lt $deadline -and -not $proc.HasExited -and $handle -eq [IntPtr]::Zero) {
    $handle = [DeskCornerCheck]::FindWindow($desktop, $deskPid)
    Start-Sleep -Milliseconds 100
  }
  if ($handle -eq [IntPtr]::Zero) { $notes += "the desk opened no window" }
  else {
    $dpi = [DeskCornerCheck]::GetDpiForWindow($handle)
    if ($dpi -gt 0) { $script:scale = $dpi / 96.0 }
    $workReady = {
      param($probes)
      $current = Current-Corners $probes.corners
      @(Sidebar-Cards $current).Count -ge $repos.Count -and @(Work-Tiles $current).Count -ge 1 -and $probes.layout.ContainsKey("open-project") -and $probes.layout.ContainsKey("palette")
    }
    if (-not (Wait-Quiet $workReady ($deadline - (Get-Date)) $settle)) { $notes += "the work screen never settled with $($repos.Count) sidebar cards and a work tile" }
    $children = @($children + (Descendants $deskPid) | Select-Object -Unique)
    $shots["plain"] = Capture $handle
    $probes = Read-Probes
    $firstCard = @(Sidebar-Cards (Current-Corners $probes.corners) | Select-Object -First 1)
    if (-not $SkipMenu -and $firstCard.Count -eq 1 -and $probes.layout.ContainsKey("open-project")) {
      $plusX = $probes.layout["open-project"] - $iconSmall / 2
      $plusY = $firstCard[0].y - 2 * $groupPad - [Math]::Max($lineCap, $addButton) / 2
      $notes += "projects + clicked at {0:N1},{1:N1}" -f $plusX, $plusY
      Click $handle $plusX $plusY
      $menuOpen = { param($probes) @(Current-Corners $probes.corners | Where-Object { $_.kind -eq "menu" }).Count -ge 1 }
      if (Wait-Quiet $menuOpen $menuTimeout $menuSettle) {
        $rect = New-Object DeskCornerRect
        [DeskCornerCheck]::GetClientRect($handle, [ref]$rect) | Out-Null
        $awayX = [int]($rect.Right * 3 / 4); $awayY = [int]($rect.Bottom / 2)
        [DeskCornerCheck]::PostMessage($handle, $WM_MOUSEMOVE, [IntPtr]0, [DeskCornerCheck]::Point($awayX, $awayY)) | Out-Null
        $notes += "pointer moved off projects + to $awayX,$awayY device px before the menu shot"
        Start-Sleep -Milliseconds $hoverSettle
        $shots["menu"] = Capture $handle
      }
      else { $notes += "the projects + menu logged no surface within $($menuTimeout.TotalSeconds) s" }
      [DeskCornerCheck]::PostMessage($handle, $WM_KEYDOWN, [IntPtr]0x1B, [IntPtr]0) | Out-Null
      Start-Sleep -Milliseconds 400
    }
    if (-not $SkipTooltip -and $probes.layout.ContainsKey("palette")) {
      $at = [DeskCornerCheck]::Point((Device ($probes.layout["palette"] - $control / 2)), (Device ($titleHeight / 2)))
      [DeskCornerCheck]::PostMessage($handle, $WM_MOUSEMOVE, [IntPtr]0, $at) | Out-Null
      Start-Sleep -Milliseconds ($settle * 2)
      [DeskCornerCheck]::PostMessage($handle, $WM_MOUSEMOVE, [IntPtr]0, $at) | Out-Null
      Start-Sleep -Milliseconds 300
      $shots["tooltip"] = Capture $handle
    }
  }
} finally {
  $children = @($children + (Descendants $deskPid) | Select-Object -Unique)
  if (-not $proc.HasExited -and $handle -ne [IntPtr]::Zero) {
    [DeskCornerCheck]::PostMessage($handle, $WM_CLOSE, [IntPtr]0, [IntPtr]0) | Out-Null
    $proc.WaitForExit(5000) | Out-Null
  }
  if (-not $proc.HasExited) { Stop-Process -Id $deskPid -Force -ErrorAction SilentlyContinue }
  foreach ($id in $children) { Stop-Process -Id $id -Force -ErrorAction SilentlyContinue }
  [DeskCornerCheck]::CloseDesktop($desktop) | Out-Null
}

foreach ($state in $shots.Keys) { $shots[$state].Save((Join-Path $root "$state.png"), [System.Drawing.Imaging.ImageFormat]::Png) }
$probes = Read-Probes
$current = Current-Corners $probes.corners
$surfaces = [ordered]@{
  "sidebar-card" = @{ shot = "plain"; found = (Sidebar-Cards $current) }
  "work-tile" = @{ shot = "plain"; found = @(Work-Tiles $current | Select-Object -First 1) }
  "projects-menu" = @{ shot = "menu"; found = @($current | Where-Object { $_.kind -eq "menu" } | Sort-Object y, x | Select-Object -First 1) }
  "search-tooltip" = @{ shot = "tooltip"; found = @($current | Where-Object { $_.kind -eq "tooltip" -and $_.y -lt 2 * $titleHeight } | Sort-Object y, x | Select-Object -First 1) }
}

$radii = @($current | ForEach-Object { $_.r } | Select-Object -Unique)
$noise = 0.0
$noiseAt = "none"
$noisePairs = 0
foreach ($state in $shots.Keys) {
  $bitmap = $shots[$state]
  foreach ($radius in $radii) {
    $by = Gap-Offset $radius
    $near = $windowInset + $by + 1
    $windowCorners = [ordered]@{
      "top-l" = @($near, $near, 1, 1); "top-r" = @(($bitmap.Width - $near), $near, -1, 1)
      "bot-l" = @($near, ($bitmap.Height - $near), 1, -1); "bot-r" = @(($bitmap.Width - $near), ($bitmap.Height - $near), -1, -1)
    }
    foreach ($name in $windowCorners.Keys) {
      $cx, $cy, $sx, $sy = $windowCorners[$name]
      $pair = Corner-Pair $bitmap $cx $cy $sx $sy $by
      if ($null -eq $pair) { continue }
      $noisePairs++
      if ($pair.diff -gt $noise) { $noise = $pair.diff; $noiseAt = "$state window $name r$radius" }
    }
  }
}
$threshold = if ($Threshold -gt 0) { $Threshold } else { $noise + [Math]::Max($margin, $noise) }
$thresholdFrom = if ($Threshold -gt 0) { "given with -Threshold" } else { "noise + max($margin, noise)" }

$deskLog = Read-Log
$resolved = [regex]::Matches($deskLog, "(?m)^desk theme (\S+ \S+)") | ForEach-Object { $_.Groups[1].Value } | Select-Object -Last 1
$backdrop = [regex]::Matches($deskLog, "(?m)^desk: shell backdrop (\S+) fill (#[0-9a-f]+)") | Select-Object -Last 1
$fill = if ($backdrop) { $backdrop.Groups[2].Value } else { "" }
$backdropText = if ($backdrop) { "$($backdrop.Groups[1].Value) fill $fill" } else { "not logged" }
$themeProblems = @([regex]::Matches($deskLog, "(?m)^theme \S+: .*$") | ForEach-Object { $_.Value.Trim() })
$failures = @()

"corner-check  exe $Exe  scale $script:scale"
"theme ${Theme}: wrote $themeFile.json (extends $themeBase, glass.tint 1, surface.window $windowColour) and config\theme `"$themeFile $themeMode`"; desk resolved `"$resolved`", shell backdrop $backdropText"
foreach ($problem in $themeProblems) { "note: $problem" }
if ($resolved -ne "$themeFile $themeMode") { $failures += "theme resolved to `"$resolved`", not `"$themeFile $themeMode`"" }
if ($fill -ne $windowColour) { $failures += "window fill $backdropText, not $windowColour" }
"clean noise {0:N1} luma, the largest gap-to-outside difference over {1} window-corner pairs {2}px in from the window edge (largest at {3})" -f $noise, $noisePairs, $windowInset, $noiseAt
"threshold {0:N1} luma, {1}" -f $threshold, $thresholdFrom
"gap pixel inside the box at floor(r*(1-1/sqrt2) - $curveClearance) device px from both edges, outside pixel the same offset outside the box on the diagonal; edge pair at ${edgeOut}px and $(2 * $edgeOut)px out"
foreach ($note in $notes) { "note: $note" }
"{0,-16} {1,-26} {2,-7} {3,8} {4,8} {5,7}  {6}" -f "surface", "box", "corner", "gap", "outside", "diff", "verdict"
foreach ($surface in $surfaces.Keys) {
  $spec = $surfaces[$surface]
  $bitmap = $shots[$spec.shot]
  $found = @($spec.found)
  if (-not $bitmap -or $found.Count -eq 0) {
    "{0,-16} {1,-26} {2,-7} {3,8} {4,8} {5,7}  FAIL missing (shot $($spec.shot): $(if ($bitmap) { 'taken' } else { 'not taken' }))" -f $surface, "--", "--", "--", "--", "--"
    $failures += "$surface missing"
    continue
  }
  if ($surface -eq "sidebar-card" -and $found.Count -ne $repos.Count) {
    "{0,-16} {1,-26} {2,-7} {3,8} {4,8} {5,7}  FAIL $($found.Count) cards for $($repos.Count) projects" -f $surface, "--", "--", "--", "--", "--"
    $failures += "$surface count $($found.Count)"
  }
  $at = 0
  foreach ($box in $found) {
    $label = if ($found.Count -gt 1) { "$surface-$at" } else { $surface }
    $at++
    $by = Gap-Offset $box.r
    $left = Device $box.x; $top = Device $box.y; $right = Device ($box.x + $box.w); $bottom = Device ($box.y + $box.h)
    $corners = [ordered]@{
      "top-l" = @($left, $top, 1, 1); "top-r" = @($right, $top, -1, 1)
      "bot-l" = @($left, $bottom, 1, -1); "bot-r" = @($right, $bottom, -1, -1)
    }
    $boxText = "{0:N0},{1:N0} {2:N0}x{3:N0} r{4:N0} @{5}" -f $box.x, $box.y, $box.w, $box.h, $box.r, $by
    foreach ($name in $corners.Keys) {
      $cx, $cy, $sx, $sy = $corners[$name]
      $pair = Corner-Pair $bitmap $cx $cy $sx $sy $by
      if ($null -eq $pair) { "{0,-16} {1,-26} {2,-7} {3,8} {4,8} {5,7}  skipped off the window" -f $label, $boxText, $name, "--", "--", "--"; $boxText = ""; continue }
      $verdict = "ok"
      if ($pair.diff -gt $threshold) { $verdict = "FAIL"; $failures += "$label $name" }
      "{0,-16} {1,-26} {2,-7} {3,8:N1} {4,8:N1} {5,7:N1}  {6}" -f $label, $boxText, $name, $pair.gap, $pair.out, $pair.diff, $verdict
      $boxText = ""
    }
    $midX = [int](($left + $right) / 2); $midY = [int](($top + $bottom) / 2)
    $sides = [ordered]@{
      "top" = @($midX, $top, 0, 1); "bottom" = @($midX, $bottom, 0, -1)
      "left" = @($left, $midY, 1, 0); "right" = @($right, $midY, -1, 0)
    }
    foreach ($name in $sides.Keys) {
      $ex, $ey, $sx, $sy = $sides[$name]
      $nearX = if ($sx -eq 0) { $ex } else { Outward $ex $sx ($edgeOut - 1) }
      $nearY = if ($sy -eq 0) { $ey } else { Outward $ey $sy ($edgeOut - 1) }
      $farX = if ($sx -eq 0) { $ex } else { Outward $ex $sx (2 * $edgeOut - 1) }
      $farY = if ($sy -eq 0) { $ey } else { Outward $ey $sy (2 * $edgeOut - 1) }
      $near = Luma $bitmap $nearX $nearY
      $far = Luma $bitmap $farX $farY
      if ($null -eq $near -or $null -eq $far) { continue }
      "{0,-16} {1,-26} {2,-7} {3,8:N1} {4,8:N1} {5,7:N1}  edge reference" -f $label, "", $name, $near, $far, ([Math]::Abs($near - $far))
    }
  }
}
"captures and desk log kept in $root"
if ($failures.Count -gt 0) {
  "failed: " + ($failures -join "; ")
  exit 1
}
exit 0
