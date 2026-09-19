# commit-msg hook.
#
# Two refusals, in this order.
#
# 1. Attribution. No agent, no bot and no harness is ever a co-author of this
#    project. A Co-authored-by or Signed-off-by trailer naming a tool is a claim
#    that something other than the owner wrote the work, and it is not true. The
#    rule has no exception and no flag to turn it off.
# 2. The message shape. Conventional commits, one short line, present tense,
#    no trailing period, no double quote because the owner commits from
#    PowerShell, and never an em dash.
#
# The hook reads the message file git hands it, as bytes, because PowerShell's
# default encoding mangles anything outside the code page.

$ErrorActionPreference = 'Stop'

$path = $args[0]
if (-not $path -or -not (Test-Path -LiteralPath $path)) { exit 0 }

$bytes = [System.IO.File]::ReadAllBytes($path)
$text = [System.Text.Encoding]::UTF8.GetString($bytes)
$lines = $text -split "`r?`n"
$body = @($lines | Where-Object { $_ -notmatch '^\s*#' })
$subject = if ($body.Count -gt 0) { $body[0] } else { '' }

function Deny([string]$why) {
    [Console]::Error.WriteLine("commit refused: $why")
    [Console]::Error.WriteLine('')
    [Console]::Error.WriteLine('  type(scope): a plain sentence')
    [Console]::Error.WriteLine('  types: feat fix docs refactor test chore bench hook')
    [Console]::Error.WriteLine('  under 72 characters, present tense, no trailing period')
    exit 1
}

foreach ($line in $body) {
    if ($line -match '(?i)^\s*(co-authored-by|signed-off-by|assisted-by|generated-by)\s*:') {
        Deny 'no attribution trailers. No agent, bot or harness is ever a co-author of this project.'
    }
    if ($line -match '(?i)\b(generated|written|authored|created|produced|assisted|helped|co-?authored|made)\b[^.]{0,40}\b(by|with|using)\b[^.]{0,20}\b(claude|anthropic|copilot|cursor|chatgpt|openai|an? (ai|agent|bot|llm|model))\b' -or
        $line -match '(?i)\b(claude|anthropic|copilot|cursor|chatgpt|openai)\b[^.]{0,20}\b(wrote|generated|authored|made|helped|assisted)\b' -or
        $line -match '(?i)\bwith\s+(claude|copilot|cursor|chatgpt)\s*$') {
        Deny "a commit message credits a tool for the work: $($line.Trim())"
    }
    if ($line -match [char]0x2014) {
        Deny 'no em dash, anywhere in this project.'
    }
    if ($line -match '"') {
        Deny 'no double quote in a commit message. The owner commits from PowerShell.'
    }
}

if ([string]::IsNullOrWhiteSpace($subject)) { Deny 'the message is empty.' }

if ($subject -notmatch '^(feat|fix|docs|refactor|test|chore|bench|hook)(\([a-z0-9._/-]+\))?!?: .+') {
    Deny "the subject is not a conventional commit: $subject"
}

if ($subject.Length -gt 72) {
    Deny "the subject is $($subject.Length) characters, the limit is 72."
}

if ($subject -match '\.$') { Deny 'the subject ends with a period.' }

exit 0
