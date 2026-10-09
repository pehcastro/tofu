$ErrorActionPreference = 'Stop'

$version = $args[0]
$message = @($args[1])
$missing = @()

$changelog = @(git show :CHANGELOG.md)
$header = '^## ' + [regex]::Escape($version) + '(\s|$)'
$start = -1
for ($i = 0; $i -lt $changelog.Count; $i++) { if ($changelog[$i] -match $header) { $start = $i; break } }
if ($start -lt 0) {
    [Console]::Error.WriteLine("release refused: CHANGELOG.md has no entry headed $version.")
    exit 1
}

$names = @()
$section = ''
$addsOrChanges = $false
for ($i = $start + 1; $i -lt $changelog.Count -and $changelog[$i] -notmatch '^## '; $i++) {
    if ($changelog[$i] -match '^### (\w+)') {
        $section = $Matches[1]
        if ($section -eq 'Added' -or $section -eq 'Changed') { $addsOrChanges = $true }
        continue
    }
    if ($section -ne 'Added' -and $section -ne 'Changed') { continue }
    foreach ($m in [regex]::Matches($changelog[$i], '`([A-Za-z_][A-Za-z0-9_.-]*)`')) { $names += $m.Groups[1].Value }
}

$docsWaived = @($message | Where-Object { $_ -match '^Docs: none, (\S+\s+){2,}\S+' }).Count -gt 0

if ($addsOrChanges -and -not $docsWaived) {
    $previous = git log -1 --format=%H --grep='^chore(release): ' HEAD
    $changedDocs = if ($previous) { @(git diff --cached --name-only $previous -- docs/) } else { @(git ls-files -- docs/) }
    if ($changedDocs.Count -eq 0) {
        $since = if ($previous) { "since $($previous.Substring(0, 8))" } else { 'in this repository' }
        $missing += "no file under docs/ changed $since. Every Added and Changed line goes into docs/ before the release, or the body says Docs: none, <reason>."
    }
}

foreach ($name in ($names | Sort-Object -Unique)) {
    git grep --cached -q -I -F -w -e $name -- docs/
    if ($LASTEXITCODE -ne 0) { $missing += "``$name`` is in the $version entry and nowhere under docs/." }
}

if ($missing.Count -eq 0) { exit 0 }
[Console]::Error.WriteLine("release refused: docs/ lags the changelog for $version.")
foreach ($line in $missing) { [Console]::Error.WriteLine("  $line") }
exit 1
