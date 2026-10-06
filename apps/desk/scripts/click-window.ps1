param(
  [string]$Title = "Tofu Desk",
  [Parameter(Mandatory = $true)][int]$X,
  [Parameter(Mandatory = $true)][int]$Y
)
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class DeskClick {
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
}
"@
$p = Get-Process | Where-Object { $_.MainWindowTitle -eq $Title } | Select-Object -First 1
if (-not $p) { "no window titled $Title"; exit 1 }
$h = $p.MainWindowHandle
$l = [IntPtr](($Y -shl 16) -bor ($X -band 0xffff))
[DeskClick]::PostMessage($h, 0x0200, [IntPtr]0, $l) | Out-Null
Start-Sleep -Milliseconds 120
[DeskClick]::PostMessage($h, 0x0201, [IntPtr]1, $l) | Out-Null
Start-Sleep -Milliseconds 60
[DeskClick]::PostMessage($h, 0x0202, [IntPtr]0, $l) | Out-Null
"clicked $Title at client $X,$Y"
