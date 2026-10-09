---
title: Reference
description: Every tofu verb and its usage line, as tofu help and each verb print them.
order: 1
updated: 2026-10-09
---

`tofu` with no arguments opens the app in the current directory. `tofu <verb>`
runs one verb and exits. Every verb that reports state takes `--json` and
prints one document, `{tofu, verb, ok, at, data, problems}`.

## Scripts and exit codes

Everything the app does can be driven from a script: the same verbs a person
types are what a script, a CI job or another agent calls, and the JSON
envelope is the same for every verb. Exit codes carry the result alone: 0 is
done, 1 is a verdict or a failure, 2 is a usage error.

## Usage lines

Run a verb with no arguments, or a wrong one, to get its usage line:

```
✗ tofu session: no subcommand
  → tofu session list|info|trace|reads|resume|rename <name|id> [--json]
```

`tofu help` lists every verb, and `tofu run --help`, `tofu sift --help`,
`tofu drive --help` and `tofu serve --help` print the long forms. The usage lines below are copied
from tofu itself.

## Commands

### Run and sessions

```
tofu run --dir <path> [arguments] <task>
tofu --continue [--json]
tofu session <name|id>, or tofu session list [--all], or tofu session info|trace|reads|resume|rename|request|find|branch <name|id> [--json]
tofu session branch <name|id> --side [--preset read|notes|files | --owns GLOB,GLOB] [--seed summary|none] [--name NAME] [--json]
tofu context [<name|id>] [--json]
tofu shells list|log|stop|restart <name> [--json]
```

### Memory

```
tofu memory [list] [--json]
tofu memory add [--scope user-local|project-local|project-global|user-global] [--kind person|project|reference] [--said "<your words>"] [--replace <id>] "<statement>"
tofu memory remove [--scope <scope>] <id>
tofu memory tree <log.jsonl> [--budget BYTES]
tofu memory zoom <log.jsonl> <id> <n>
tofu memory recall <log.jsonl> <regex>
```

`add` and `remove` each take `[--dir project]`. See
[Memory](/docs/features/memory).

### Accounts and models

```
tofu login llm <claude-sub|codex-sub> [--paste] [--json]
tofu login llm meta [--json]
tofu login classifier <openrouter|typesafe> [--json]
tofu login search brave [--json]
tofu logout <llm|classifier|search> <provider> [number] [--json]
tofu login --status [--json] [--redact]
tofu login --disable|--enable <number from tofu login --status>
tofu usage [--history] [--json]
tofu models [reload] [--json]
tofu agents [--json] | add | set | remove
tofu agents add [--global] [--dir project] <name> --description d --model source/model [--tools a,b]
tofu agents set [--global] [--dir project] <name> <source/model>
tofu agents remove [--global] [--dir project] <name>
```

### Library and settings

```
tofu rules list|check|fired|index|add|off|remove
tofu rules index "<task>" [path...] [--task kind] [--role orchestrator|sub-agent] [--library dir] [--dir project] [--json]
tofu rules add [--global] [--dir project] [--replace] [--concern c] [--json] <id> "<text>"
tofu rules off|remove [--global] [--dir project] [--json] <id>
tofu library [resolve <name>] [--dir <path>] [--json]
tofu lint comments [path] [--json]
tofu reload [--json]
tofu settings [get <key> | set [--scope global|project] <key> <value>] [--json]
```

### Judgment

```
tofu judge [--dry-run] [--no-cache] [--no-rule] [--json] < request.json
tofu judge --lint file [--json]
tofu check "<command>" [--quiet] [--json]
tofu sift [--arm length|signpost|brevity|jev] [--task text] [--restore] [--no-log] [--json] < text
tofu why <id> | --last [n] [--point name] [--state | --json]
tofu label <id> allow|ask|deny | tofu label --last allow|ask|deny [--json]
tofu replay --point name [--set threshold=value]... [--since 7d] [--verbose] [--json]
```

### Diagnosis

```
tofu doctor [--imports] [--json]
tofu version [--json]
tofu changelog [--all] [--json]
tofu docs [topic | "a few words"] [--json]
tofu frame [NAME] [--width N] [--height N] [--plain] [--list]
tofu drive [SCRIPT] [--dir PATH] [--home PATH] [--cassette PATH] [--width N] [--height N] [--plain] [--fresh | --continue] [ARM]
tofu migrate [--dry-run] [--json]
```

### Frontends

```
tofu serve --stdio [--dir PATH] [--cassette PATH]
tofu serve --schema
```

`tofu serve --stdio` speaks `tofu.host/1`, JSON-RPC 2.0 one object a line on
standard input and output, for the desk app or any program that starts tofu.
`tofu serve --schema` prints the JSON Schema of every line, and `tofu docs serve`
explains the requests, the events and the approvals. See [Serving tofu to
another program](/docs/cli/serve).

`tofu migrate` has no undo; run `--dry-run` first.

### Browser

```
tofu browser [install | uninstall | tabs | build | recipes | open <url> | close <tab id>] [--json]
tofu browser observe [--all] | click <ref> | fill <ref> <text> | select <ref> <option> | press <key> | scroll [<ref>] [up|down] | back   --tab <id> [--json]
tofu browser batch [<steps.json> | -] [--tab <id>] [--json]
tofu browser bench [--jev] [--n 12] [--rows file] [--json]
tofu browser motion capture <scenario.json> [--json]
```
