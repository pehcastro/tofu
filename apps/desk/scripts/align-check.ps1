param(
  [string]$Exe = ""
)
$ErrorActionPreference = "Stop"
$desk = Split-Path $PSScriptRoot -Parent
if (-not $Exe) { $Exe = Join-Path $desk "target_flows\motion\tofu-desktop.exe" }
if (-not (Test-Path $Exe)) { "no desk at $Exe"; exit 2 }
$Exe = (Resolve-Path $Exe).Path
$tolerance = 1.0
$timeout = [TimeSpan]::FromSeconds(60)

function Read-Const([string]$file, [string]$name) {
  $text = Get-Content -Raw -LiteralPath (Join-Path $desk $file)
  if ($text -notmatch "const $name\s*:\s*f32\s*=\s*([0-9.]+)\s*;") { throw "no const $name in $file" }
  [double]$Matches[1]
}
$padX = Read-Const "crates\ui\src\components\sidebar.rs" "SIDEBAR_PAD_X"
$cardPad = Read-Const "crates\ui\src\components\sidebar.rs" "CARD_PAD"
$rowPadX = Read-Const "crates\ui\src\components\size.rs" "ROW_PAD_X"
$width = Read-Const "crates\ui\src\metrics.rs" "SIDEBAR_WIDTH"
$left = $padX + $cardPad + $rowPadX
$right = $width + 2 * $padX - $left
$edges = @{ left = $left; right = $right }
$expected = [ordered]@{
  "sidebar-toggle" = "left"; "palette" = "right"
  "tool-editor" = "left"; "tool-boardy" = "left"
  "projects-caption" = "left"; "open-project" = "right"
  "project-name" = "left"; "project-chevron" = "right"; "new-session" = "right"; "inactive-count" = "right"
  "project-name-1" = "left"; "project-chevron-1" = "right"; "new-session-1" = "right"; "inactive-count-1" = "right"
  "branch-icon" = "left"
}

Add-Type @"
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
public class DeskAlignCheck {
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
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
}
"@
$WM_CLOSE = 0x0010

$root = Join-Path ([IO.Path]::GetTempPath()) ("desk-align-check-" + [guid]::NewGuid().ToString("N"))
$userHome = Join-Path $root "home"
New-Item -ItemType Directory -Force -Path (Join-Path $userHome ".tofu\desk") | Out-Null
$repos = foreach ($name in @("alpha", "beta")) {
  $repo = Join-Path $root $name
  New-Item -ItemType Directory -Force -Path $repo | Out-Null
  Set-Content -LiteralPath (Join-Path $repo "README.md") -Value $name
  git -C $repo init -q -b main | Out-Null
  git -C $repo add README.md | Out-Null
  git -C $repo -c user.name=align -c user.email=align@localhost commit -q -m init | Out-Null
  if ($LASTEXITCODE -ne 0) { "could not make the git repo $repo"; exit 2 }
  $repo
}
$errLog = Join-Path $root "desk.err.log"

function Read-Log {
  if (-not (Test-Path $errLog)) { return "" }
  $stream = [IO.File]::Open($errLog, "Open", "Read", "ReadWrite")
  try { (New-Object IO.StreamReader($stream)).ReadToEnd() } finally { $stream.Dispose() }
}

function Read-Layout {
  $found = @{}
  foreach ($line in ((Read-Log) -split "`r?`n")) {
    if ($line -match "^layout (\S+) edge=(left|right) x=(-?[0-9.]+)") {
      if (-not $found.ContainsKey($Matches[1])) { $found[$Matches[1]] = @{ edge = $Matches[2]; x = [double]$Matches[3] } }
    }
  }
  $found
}

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

$saved = @{ USERPROFILE = $env:USERPROFILE; APPDATA = $env:APPDATA; DESK_LAYOUT_LOG = $env:DESK_LAYOUT_LOG; DESK_FIRST_RUN = $env:DESK_FIRST_RUN }
$env:USERPROFILE = $userHome
$env:APPDATA = Join-Path $userHome "AppData\Roaming"
$env:DESK_LAYOUT_LOG = "1"
$env:DESK_FIRST_RUN = "ready"
$desktopName = "desk-align-check-" + [guid]::NewGuid().ToString("N")
try {
  $desktop = [DeskAlignCheck]::NewDesktop($desktopName)
} catch { "could not make a hidden desktop: $($_.Exception.Message)"; exit 2 }
$logStream = [IO.File]::Open($errLog, "Create", "Write", "ReadWrite")
try {
  $line = "`"$Exe`" " + (($repos | ForEach-Object { "`"$_`"" }) -join " ")
  $deskPid = [DeskAlignCheck]::Start($desktopName, $line, $desk, $logStream.SafeFileHandle.DangerousGetHandle())
  $proc = Get-Process -Id $deskPid
} catch {
  "could not start the desk on the hidden desktop: $($_.Exception.Message)"; [DeskAlignCheck]::CloseDesktop($desktop) | Out-Null; exit 2
} finally {
  $logStream.Dispose()
  foreach ($name in $saved.Keys) { Set-Item -Path "env:$name" -Value $saved[$name] -ErrorAction SilentlyContinue; if ($null -eq $saved[$name]) { Remove-Item "env:$name" -ErrorAction SilentlyContinue } }
}

$children = @()
$handle = [IntPtr]::Zero
$deadline = (Get-Date) + $timeout
try {
  while ((Get-Date) -lt $deadline -and -not $proc.HasExited) {
    if ($handle -eq [IntPtr]::Zero) { $handle = [DeskAlignCheck]::FindWindow($desktop, $deskPid) }
    $children = @($children + (Descendants $deskPid) | Select-Object -Unique)
    $layout = Read-Layout
    if (@($expected.Keys | Where-Object { -not $layout.ContainsKey($_) }).Count -eq 0) { break }
    Start-Sleep -Milliseconds 250
  }
  if ($handle -eq [IntPtr]::Zero) { "the desk opened no window" }
} finally {
  $children = @($children + (Descendants $deskPid) | Select-Object -Unique)
  if (-not $proc.HasExited -and $handle -ne [IntPtr]::Zero) {
    [DeskAlignCheck]::PostMessage($handle, $WM_CLOSE, [IntPtr]0, [IntPtr]0) | Out-Null
    $proc.WaitForExit(5000) | Out-Null
  }
  if (-not $proc.HasExited) { Stop-Process -Id $deskPid -Force -ErrorAction SilentlyContinue }
  foreach ($id in $children) { Stop-Process -Id $id -Force -ErrorAction SilentlyContinue }
  [DeskAlignCheck]::CloseDesktop($desktop) | Out-Null
}

$layout = Read-Layout
"align-check  exe $Exe  left edge x=$left  right edge x=$right  tolerance $tolerance px"
"{0,-20} {1,-6} {2,9} {3,9}  {4}" -f "control", "edge", "x", "expected", "verdict"
$failures = @()
foreach ($name in $expected.Keys) {
  $edge = $expected[$name]
  $want = $edges[$edge]
  $got = $layout[$name]
  if (-not $got) {
    "{0,-20} {1,-6} {2,9} {3,9:N1}  FAIL missing from the log" -f $name, $edge, "--", $want
    $failures += "$name missing"
    continue
  }
  $verdict = "ok"
  if ($got.edge -ne $edge) { $verdict = "FAIL logged edge $($got.edge)"; $failures += "$name logged on the $($got.edge) edge" }
  elseif ([Math]::Abs($got.x - $want) -gt $tolerance) { $verdict = "FAIL off by {0:N1} px" -f ($got.x - $want); $failures += "$name off by {0:N1} px" -f ($got.x - $want) }
  "{0,-20} {1,-6} {2,9:N1} {3,9:N1}  {4}" -f $name, $edge, $got.x, $want, $verdict
}
foreach ($name in ($layout.Keys | Where-Object { -not $expected.Contains($_) } | Sort-Object)) {
  "{0,-20} {1,-6} {2,9:N1} {3,9}  not checked" -f $name, $layout[$name].edge, $layout[$name].x, "--"
}
if ($failures.Count -gt 0) {
  "failed: " + ($failures -join "; ")
  "desk log kept at $errLog"
  exit 1
}
Remove-Item -Recurse -Force -Path $root -ErrorAction SilentlyContinue
exit 0
