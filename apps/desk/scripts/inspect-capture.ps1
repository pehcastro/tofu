param([Parameter(Mandatory = $true)][string]$Path)
Add-Type -AssemblyName System.Drawing
$b = [System.Drawing.Bitmap]::FromFile($Path)
function Runs([int]$y0, [int]$y1) {
  $runs = @()
  $start = -1
  for ($x = 0; $x -lt $b.Width; $x++) {
    $lit = $false
    for ($y = $y0; $y -le $y1; $y++) {
      $c = $b.GetPixel($x, $y)
      if (($c.R + $c.G + $c.B) -gt 150) { $lit = $true; break }
    }
    if ($lit -and $start -lt 0) { $start = $x }
    if (-not $lit -and $start -ge 0) { $runs += "$start-$($x - 1)"; $start = -1 }
  }
  if ($start -ge 0) { $runs += "$start-$($b.Width - 1)" }
  $runs -join " "
}
"size $($b.Width)x$($b.Height)"
"title band y 0-47: $(Runs 0 47)"
"sidebar band y 50-90: $(Runs 50 90)"
"status band y $($b.Height - 38)-$($b.Height - 1): $(Runs ($b.Height - 38) ($b.Height - 1))"
$rows = @()
for ($y = 0; $y -lt $b.Height; $y++) {
  $lit = $false
  for ($x = 0; $x -lt $b.Width; $x += 2) {
    $c = $b.GetPixel($x, $y)
    if (($c.R + $c.G + $c.B) -gt 150) { $lit = $true; break }
  }
  if ($lit) { $rows += $y }
}
"lit rows: $($rows[0])..$($rows[-1]) count $($rows.Count)"
$corner = $b.GetPixel(0, 0)
"corner pixel rgba $($corner.R),$($corner.G),$($corner.B),$($corner.A)"
$b.Dispose()
