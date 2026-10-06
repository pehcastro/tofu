param(
  [Parameter(Mandatory = $true)][string]$Log,
  [Parameter(Mandatory = $true)][string]$Cargo
)
$ErrorActionPreference = "Stop"
$watch = [System.Diagnostics.Stopwatch]::StartNew()
$p = Start-Process cargo -ArgumentList $Cargo -NoNewWindow -PassThru -RedirectStandardError $Log -RedirectStandardOutput "$Log.out"
$null = $p.Handle
$p.PriorityClass = "Idle"
$p.WaitForExit()
$watch.Stop()
$summary = "cargo $Cargo exit $($p.ExitCode) wall $([math]::Round($watch.Elapsed.TotalSeconds, 1))s"
Add-Content -Path "$Log.wall" -Value $summary
$summary
exit $p.ExitCode
