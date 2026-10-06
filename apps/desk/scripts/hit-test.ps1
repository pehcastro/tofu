param([string]$Title = "Tofu Desk")
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class DeskHit {
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern IntPtr SendMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h, ref POINT p);
  [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
}
"@
$names = @{ 0 = "HTNOWHERE"; 1 = "HTCLIENT"; 2 = "HTCAPTION"; 8 = "HTMINBUTTON"; 9 = "HTMAXBUTTON"; 20 = "HTCLOSE" }
$p = Get-Process | Where-Object { $_.MainWindowTitle -eq $Title } | Select-Object -First 1
if (-not $p) { "no window titled $Title"; exit 1 }
$h = $p.MainWindowHandle
$points = @(
  @("sidebar toggle", 20, 20),
  @("empty middle", 600, 20),
  @("minimize", 1184, 20),
  @("maximize", 1220, 20),
  @("close", 1256, 20),
  @("status cron", 1247, 782)
)
foreach ($pt in $points) {
  $client = [IntPtr](($pt[2] -shl 16) -bor ($pt[1] -band 0xffff))
  [DeskHit]::PostMessage($h, 0x0200, [IntPtr]0, $client) | Out-Null
  Start-Sleep -Milliseconds 150
  $s = New-Object DeskHit+POINT
  $s.X = $pt[1]; $s.Y = $pt[2]
  [DeskHit]::ClientToScreen($h, [ref]$s) | Out-Null
  $screen = [IntPtr](($s.Y -shl 16) -bor ($s.X -band 0xffff))
  $code = [int][DeskHit]::SendMessage($h, 0x0084, [IntPtr]0, $screen)
  "{0,-15} client {1},{2} -> {3} ({4})" -f $pt[0], $pt[1], $pt[2], $code, $names[$code]
}
