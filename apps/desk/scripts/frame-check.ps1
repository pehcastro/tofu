param(
  [Parameter(Mandatory = $true)][string]$Screen,
  [string]$Exe = "",
  [int[]]$Rows = @()
)
$ErrorActionPreference = "Stop"
$desk = Split-Path $PSScriptRoot -Parent
if (-not $Exe) { $Exe = Join-Path $desk "target_dev\motion\tofu-desktop.exe" }
if (-not (Test-Path $Exe)) { "no desk at $Exe"; exit 2 }
if (-not (Select-String -LiteralPath $Exe -SimpleMatch "TOFU_DESK_FRAME_CHECK" -Quiet)) { "$Exe was built without the frame check: rebuild it with the profiler feature from this tree"; exit 2 }
if ($Rows.Count -ne 0 -and $Rows.Count -ne 3) { "-Rows takes x,y1,y2"; exit 2 }
$p95Limit = 8.3
$timeout = [TimeSpan]::FromSeconds(60)
Add-Type @"
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
public class DeskFrameCheck {
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h, ref POINT p);
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
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
  [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
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
$WM_MOUSEMOVE = 0x0200
$WM_MOUSEWHEEL = 0x020A
$WM_MOUSELEAVE = 0x02A3
$WM_CLOSE = 0x0010

$userHome = Join-Path ([IO.Path]::GetTempPath()) ("desk-frame-check-" + [guid]::NewGuid().ToString("N"))
$copied = Join-Path $userHome ".tofu\desk"
New-Item -ItemType Directory -Force -Path $copied | Out-Null
$real = Join-Path $env:USERPROFILE ".tofu\desk"
foreach ($name in @("themes", "config")) {
  $from = Join-Path $real $name
  if (Test-Path $from) { Copy-Item -Recurse -Force -Path $from -Destination $copied }
}
$errLog = Join-Path $userHome "desk.err.log"

function Read-Log {
  if (-not (Test-Path $errLog)) { return "" }
  $stream = [IO.File]::Open($errLog, "Open", "Read", "ReadWrite")
  try { (New-Object IO.StreamReader($stream)).ReadToEnd() } finally { $stream.Dispose() }
}

function Wait-Phase([string]$name, [datetime]$deadline) {
  while ((Get-Date) -lt $deadline) {
    if ((Read-Log) -match "frame-check phase $name") { return $true }
    if ($script:proc.HasExited) { return $false }
    Start-Sleep -Milliseconds 50
  }
  $false
}

function Client-Point([int]$x, [int]$y) { [IntPtr](($y -shl 16) -bor ($x -band 0xffff)) }

function Screen-Point([int]$x, [int]$y) {
  $point = New-Object DeskFrameCheck+POINT
  $point.X = $x; $point.Y = $y
  [DeskFrameCheck]::ClientToScreen($script:handle, [ref]$point) | Out-Null
  Client-Point $point.X $point.Y
}

$savedProfile = $env:USERPROFILE
$savedAppData = $env:APPDATA
$savedCheck = $env:TOFU_DESK_FRAME_CHECK
$env:USERPROFILE = $userHome
$env:APPDATA = Join-Path $userHome "AppData\Roaming"
$env:TOFU_DESK_FRAME_CHECK = "1"
$desktopName = "desk-frame-check-" + [guid]::NewGuid().ToString("N")
try {
  $desktop = [DeskFrameCheck]::NewDesktop($desktopName)
} catch { "could not make a hidden desktop: $($_.Exception.Message)"; exit 2 }
$logStream = [IO.File]::Open($errLog, "Create", "Write", "ReadWrite")
try {
  $deskPid = [DeskFrameCheck]::Start($desktopName, "`"$Exe`" --screen `"$Screen`"", $desk, $logStream.SafeFileHandle.DangerousGetHandle())
  $script:proc = Get-Process -Id $deskPid
} catch {
  "could not start the desk on the hidden desktop: $($_.Exception.Message)"; [DeskFrameCheck]::CloseDesktop($desktop) | Out-Null; exit 2
} finally {
  $logStream.Dispose()
  $env:USERPROFILE = $savedProfile
  $env:APPDATA = $savedAppData
  $env:TOFU_DESK_FRAME_CHECK = $savedCheck
}
$deadline = (Get-Date) + $timeout
$script:handle = [IntPtr]::Zero
while ($script:handle -eq [IntPtr]::Zero -and (Get-Date) -lt $deadline -and -not $proc.HasExited) {
  Start-Sleep -Milliseconds 100
  $script:handle = [DeskFrameCheck]::FindWindow($desktop, $deskPid)
}
if ($script:handle -eq [IntPtr]::Zero) { "the desk opened no window"; Read-Log; if (-not $proc.HasExited) { Stop-Process -Id $deskPid -Force }; [DeskFrameCheck]::CloseDesktop($desktop) | Out-Null; exit 2 }
$rect = New-Object DeskFrameCheck+RECT
[DeskFrameCheck]::GetClientRect($handle, [ref]$rect) | Out-Null
$width = $rect.Right - $rect.Left
$height = $rect.Bottom - $rect.Top
if ($Rows.Count -eq 3) { $x, $top, $bottom = $Rows } else { $x = [int]($width / 2); $top = [int]($height * 0.15); $bottom = [int]($height * 0.85) }
"frame-check $Screen  exe $Exe  client ${width}x${height}  column x=$x y=$top..$bottom  hidden desktop $desktopName"

$ran = $false
if (Wait-Phase "park" $deadline) {
  [DeskFrameCheck]::PostMessage($handle, $WM_MOUSELEAVE, [IntPtr]0, [IntPtr]0) | Out-Null
  if (Wait-Phase "hover" $deadline) {
    $y = $top; $step = 4
    $clock = [Diagnostics.Stopwatch]::StartNew()
    while ($true) {
      [DeskFrameCheck]::PostMessage($handle, $WM_MOUSEMOVE, [IntPtr]0, (Client-Point $x $y)) | Out-Null
      $y += $step
      if ($y -ge $bottom -or $y -le $top) { $step = -$step }
      Start-Sleep -Milliseconds 8
      if ($clock.ElapsedMilliseconds -ge 100) {
        $clock.Restart()
        if ((Read-Log) -match "frame-check phase scroll" -or $proc.HasExited) { break }
      }
    }
    $wheelAt = Screen-Point $x ([int](($top + $bottom) / 2))
    [DeskFrameCheck]::PostMessage($handle, $WM_MOUSEMOVE, [IntPtr]0, (Client-Point $x ([int](($top + $bottom) / 2)))) | Out-Null
    $notch = 0
    while (-not $proc.HasExited -and (Get-Date) -lt $deadline) {
      $delta = if ([int]($notch / 15) % 2 -eq 0) { -120 } else { 120 }
      [DeskFrameCheck]::PostMessage($handle, $WM_MOUSEWHEEL, [IntPtr]($delta -shl 16), $wheelAt) | Out-Null
      $notch++
      Start-Sleep -Milliseconds 30
    }
    $ran = $proc.HasExited
  }
}
if (-not $proc.HasExited) {
  [DeskFrameCheck]::PostMessage($handle, $WM_CLOSE, [IntPtr]0, [IntPtr]0) | Out-Null
  if (-not $proc.WaitForExit(5000)) { Stop-Process -Id $proc.Id -Force }
}
[DeskFrameCheck]::CloseDesktop($desktop) | Out-Null

$log = Read-Log
$results = @{}
foreach ($line in ($log -split "`r?`n")) {
  if ($line -match "^frame-check result (.*)$") {
    $result = $Matches[1] | ConvertFrom-Json
    $results[$result.scenario] = $result
  } elseif ($line -match "^frame-check settle") {
    $line
  }
}
$failed = -not $ran
"{0,-7} {1,7} {2,7} {3,7} {4,7} {5,7} {6,13} {7,8}  {8}" -f "phase", "frames", "p50", "p95", "p99", "max", "allocs/frame", "peak MB", "verdict"
foreach ($phase in @("idle", "hover", "scroll")) {
  $r = $results[$phase]
  if (-not $r) { "{0,-7} missing: the desk never reported it" -f $phase; $failed = $true; continue }
  $why = @()
  if ($r.p95_ms -ge $p95Limit) { $why += "p95 >= $p95Limit ms" }
  if ($phase -eq "idle" -and $r.frames -gt 0) { $why += "drew $($r.frames) frames while idle" }
  $allocs = if ($null -eq $r.allocations_per_frame) { "n/a" } else { "{0:N1}" -f $r.allocations_per_frame }
  if ($phase -eq "idle" -and $null -ne $r.allocations_per_frame -and $r.allocations_per_frame -gt 0) { $why += "allocates while idle" }
  $verdict = if ($why.Count -eq 0) { "ok" } else { "FAIL " + ($why -join ", ") }
  if ($why.Count -gt 0) { $failed = $true }
  "{0,-7} {1,7} {2,7:N2} {3,7:N2} {4,7:N2} {5,7:N2} {6,13} {7,8:N1}  {8}" -f $phase, $r.frames, $r.p50_ms, $r.p95_ms, $r.p99_ms, $r.max_ms, $allocs, $r.peak_working_set_mb, $verdict
}
if ($results.Values | Where-Object { $null -eq $_.allocations_per_frame }) { "allocs n/a: this desk build installs no counting allocator, so idle allocations were not checked" }
if ($failed) { "desk log kept at $errLog"; exit 1 }
Remove-Item -Recurse -Force -Path $userHome -ErrorAction SilentlyContinue
exit 0
