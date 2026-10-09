param(
  [Parameter(Mandatory = $true)][int]$ProcessId,
  [Parameter(Mandatory = $true)][ValidateSet("capture", "click", "settings", "type")][string]$Do,
  [string]$Out = "",
  [string]$Text = "",
  [int]$X = 0,
  [int]$Y = 0
)
$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing
Add-Type @"
using System;
using System.Runtime.InteropServices;
public struct DeskRect { public int Left, Top, Right, Bottom; }
public class DeskWindow {
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out DeskRect r);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr dc, uint flags);
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern void keybd_event(byte key, byte scan, uint flags, UIntPtr extra);
}
"@
$process = Get-Process -Id $ProcessId
$window = $process.MainWindowHandle
if ($window -eq [IntPtr]::Zero) { "pid $ProcessId has no window"; exit 1 }
switch ($Do) {
  "capture" {
    $rect = New-Object DeskRect
    [DeskWindow]::GetWindowRect($window, [ref]$rect) | Out-Null
    $bitmap = New-Object System.Drawing.Bitmap ($rect.Right - $rect.Left), ($rect.Bottom - $rect.Top)
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    $dc = $graphics.GetHdc()
    [DeskWindow]::PrintWindow($window, $dc, 2) | Out-Null
    $graphics.ReleaseHdc($dc)
    $bitmap.Save($Out, [System.Drawing.Imaging.ImageFormat]::Png)
    "captured pid $ProcessId $($bitmap.Width)x$($bitmap.Height) to $Out"
  }
  "click" {
    $at = [IntPtr](($Y -shl 16) -bor ($X -band 0xffff))
    [DeskWindow]::PostMessage($window, 0x0200, [IntPtr]0, $at) | Out-Null
    [DeskWindow]::PostMessage($window, 0x0201, [IntPtr]1, $at) | Out-Null
    [DeskWindow]::PostMessage($window, 0x0202, [IntPtr]0, $at) | Out-Null
    "clicked pid $ProcessId at $X,$Y"
  }
  "type" {
    foreach ($char in $Text.ToCharArray()) {
      [DeskWindow]::PostMessage($window, 0x0102, [IntPtr][int]$char, [IntPtr]1) | Out-Null
    }
    "typed $Text into pid $ProcessId"
  }
  "settings" {
    [DeskWindow]::SetForegroundWindow($window) | Out-Null
    Start-Sleep -Milliseconds 300
    [DeskWindow]::keybd_event(0x11, 0, 0, [UIntPtr]::Zero)
    [DeskWindow]::keybd_event(0xBC, 0, 0, [UIntPtr]::Zero)
    [DeskWindow]::keybd_event(0xBC, 0, 2, [UIntPtr]::Zero)
    [DeskWindow]::keybd_event(0x11, 0, 2, [UIntPtr]::Zero)
    "sent ctrl+comma to pid $ProcessId"
  }
}
