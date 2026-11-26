---
title: Reference
description: Every tofu verb and its usage line, as tofu help and each verb print them.
order: 1
updated: 2026-11-25
---

`tofu` with no arguments opens the app in the current directory. `tofu <verb>`
runs one verb and exits. Every verb that reports state takes `--json` and
prints one document, `{tofu, verb, ok, at, data, problems}`.

## Scripts and exit codes

Everything the app does can be driven from a script: the same verbs a person
types are what a script, a CI job or another agent calls, and the JSON
envelope is the same for every verb. Exit codes carry the result alone: 0 is
done, 1 is a verdict or a failure, 2 is a usage error.

A verb is cheap to call in a loop: tofu starts in about 6 ms of package init.

## Usage lines

Run a verb with no arguments, or a wrong one, to get its usage line:

```
✗ tofu session: no subcommand
  → tofu session <name|id|head>, or tofu session list [--all], or ...
```

A verb or a flag tofu does not know gets its usage line and then the
closest one it does know, as `did you mean tofu session`.

`tofu help` lists every verb. Every verb answers `--help` with its usage
lines and exit 0, such as `tofu boardy new --help`, and `tofu run --help`,
`tofu sift --help`, `tofu drive --help`, `tofu host --help` and
`tofu serve --help` print the long forms. The usage lines below are the
ones `--help` prints.

## Commands

### Run and sessions

```
tofu run --dir <path> [arguments] [--command <names>] [<task>]
tofu --continue
tofu --temporary-project
tofu --profile
tofu session <name|id|head>
tofu session list [--all]
tofu session clean [--dry-run]
tofu session set <name|id|head> --model source/model
tofu session work <name|id|head> --ticket ID
tofu session info|trace|transcript|drain|reads|resume|rename|find|branch <name|id|head> [--json]
tofu session request <name|id|head> [<request id>|last|--has kind/source] [--json]
tofu session branch <name|id|head> --side [--preset read|notes|files | --owns GLOB,GLOB] [--seed summary|none] [--name NAME] [--model ID] [--effort LEVEL] [--at EVENT] [--json]
tofu session drain [name|id|head] [--claude <transcript.jsonl>] [--json]
tofu session find <name|id|head> [--tool T] [--command C] [--file F] [--text T] [--failed] [--agent A] [--since 2h|RFC3339] [--until 2h|RFC3339] [--json]
tofu session trace <name|id|head> [--since 1h] [--from ORIGIN|T] [--to T] [--agent NAME|lead] [--turn ID] [--kind K,K] [--grep TEXT] [--last N] [--wide] [--full] [--json]
tofu session transcript <name|id|head> [--from N] [--to N] [--md] [--json]
tofu host ensure|status|stop|run [--dir PATH] [--json]
tofu projects [--json]
tofu context [<name|id|head>] [--json]
tofu shells list|log|stop|restart <name> [--json]
tofu undo [N] [--dir path] [--session id] [--turn id] [--force] [--dry-run] [--json]
```

`tofu run --command` runs a slash command the way the app's `/name` does,
each as its own turn of one session in the order given and the task last:
repeat the flag or join the names with commas, as in
`--command generate-map,hunt`. The names are `simplify`, `debug`, `hunt`,
`code-review`, `generate-map` and `restructure`. `tofu run --session <handle>`
makes the task the next turn of that session, with its conversation and the
prefix it first sent.

`tofu host` is the one process per project that runs its sessions. The app,
the desk and `tofu serve` start it on demand and join it; `ensure` starts it
when none answers, `status` prints it and its members, and `stop` ends it.
See [Sessions](/docs/features/sessions).

`tofu projects` lists every project tofu knows, the one opened last first,
each with its sessions and whether one is working, and starts no host.

`--temporary-project` registers a project tofu has not seen as temporary,
`"temporary": true` in `~/.tofu/projects.json`. The first tofu started two
hours after its last use removes its folder under `~/.tofu/projects`, with its
sessions, undo history and logs, and its registry entry, unless a running tofu
still has it open; the project directory
itself is never touched. Opening it once without the flag keeps it for good.

`--profile`, with the app, `--continue` or `tofu run`, writes Go's own
[`runtime/pprof`](https://pkg.go.dev/runtime/pprof) profiles into
`~/.tofu/projects/<project>/profile/<start time>-<pid>`, the folder it prints
when it starts: every 3 minutes and at exit, `NNNN-cpu.pprof` for the minutes
since the last one, `NNNN-heap.pprof` and `NNNN-goroutines.txt`. The last 5
snapshots and the last 5 runs are kept; older ones are removed as new ones are
written. Read one with `go tool pprof`. `TOFU_PPROF=<dir>` writes one
whole-run `cpu.pprof`, `heap-peak.pprof`, `heap-end.pprof` and `memstats.json`.

### Memory

```
tofu memory [list] [--json]
tofu memory add [--scope user-local|project-local|project-global|user-global] [--kind person|project|reference] [--said "<your words>"] [--replace <id>] "<statement>"
tofu memory remove [--scope <scope>] <id>, each with [--dir project]
tofu memory tree <log.jsonl> [--budget BYTES]
tofu memory zoom <log.jsonl> <id> <n>
tofu memory recall <log.jsonl> <regex>
```

`add` and `remove` each take `[--dir project]`. See
[Memory](/docs/features/memory).

### Accounts and models

```
tofu login llm <claude-sub|codex-sub> [--paste]
tofu login llm <meta|openrouter>
tofu login classifier <openrouter|typesafe>
tofu login search brave
tofu login --status [--json] [--redact]
tofu login --disable|--enable <number from tofu login --status>
tofu logout <llm|classifier|search> <provider> [number from tofu login --status]
tofu usage [--history] [--json]
tofu models [update] [--json]
tofu models bind <orchestrator|classifier|sub-agent> <provider/model> [--json]
tofu agents [--json] | add | set | remove
tofu agents add [--global] [--dir project] <name> --description d --model source/model [--tools a,b]
tofu agents set [--global] [--dir project] <name> <source/model>
tofu agents remove [--global] [--dir project] <name>
```

### Library and settings

```
tofu rules list|show|check|fired|index|overrides|add|off|restore|remove|why
tofu rules list [--library dir] [--dir project] [--json]
tofu rules show <id> [--library dir] [--dir project] [--json]
tofu rules overrides [--library dir] [--dir project] [--json]
tofu rules check [path] [--library dir] [--dir project] [--json]
tofu rules index "<task>" [path...] [--task kind] [--role orchestrator|sub-agent] [--agent name] [--library dir] [--dir project] [--json]
tofu rules add [--global|--project] [--dir project] [--replace] [--concern c] [--json] <id> "<text>" [--reason "<why>"]
tofu rules off [--global|--project] [--dir project] [--json] <id> --reason "<why>"
tofu rules remove|restore [--global|--project] [--dir project] [--json] <id>
tofu rules why <session> [turn] [--json]
tofu library [resolve <name>] [--dir <path>] [--json]
tofu library reach [--never] [--json]
tofu lint comments [path] [--json]
tofu reload [--json]
tofu settings [get <key> | set [--scope global|project] <key> <value>] [--json]
tofu hooks [trust] [--json]
tofu learn scan [--chain <session or family>|--last N|--global] [--local] [--all] [--dir project] [--json]
tofu learn show|upstream <n>
tofu learn apply <n> [--scope user-local|project-local|project-global|user-global] [--dir project]
tofu learn reject <n> --reason "<why>"
tofu learn upstream --list
```

`tofu rules why <session>` names, for each turn and agent of the session,
every rule with its state, such as `fired` or `switched off`, and why.
`tofu library reach` counts, across your sessions, how many reached and how
many held each rule, reference, skill and sub-agent, and how often each tool
was called, with when it was last seen; `--never` keeps the ones no session
used.

### Scratchpad

```
tofu scratch [clean [--session X] [--cache] [--dry-run] | open] [--json]
```

### Boards

```
tofu boardy init --key KEY [--name text] [--mode default|empty|custom] [--hooks id,id] [--json]
tofu boardy new [--board KEY | --parent ID] [--type story|task|bug|subtask] [--priority P0..P4] [--status s] [--points n] [--size S|M|L] [--spec path] [--owns a,b]... [--skills a,b]... [--labels docs-only,...]... [--epic E] [--sprint S] [--problem text] [--scope text] --before text --driven text --still-works line... --error-case text [--feasibility text] [--acceptance line]... [--as name] <title> [--json]
tofu boardy list [--board KEY] [--status s] [--all] [--fields id,status,assignee,owns] [--json]
tofu boardy show <ticket> [--thread] [--json]
tofu boardy move <ticket> <status> [--reason text] [--bypass <hook> --why text] [--as name] [--json]
tofu boardy log <ticket> [<text>] [--evidence [--line n] | --before <what you ran and what it printed> [--late <commit or build>]] [--as name] [--json]
tofu boardy set <ticket | request [--board KEY]> [--title text] [--owns a,b]... [--skills a,b]... [--epic E] [--sprint S] [--acceptance line]... [--reason text] [--before text] [--driven text] [--still-works line]... [--error-case text] [--feasibility text] [--as name] [--json]
tofu boardy lint [--board KEY] [--json]
tofu boardy boards [--json]
tofu boardy held <path> [--json]
tofu boardy manager add|remove|list [name] [--board KEY] [--as name] [--json]
tofu boardy triage list|show|accept|drop [request] [--board KEY] [--reason text] [--as name] [--json]
tofu boardy assign <ticket> <session or name> [--as name] [--json]
tofu boardy hand <ticket> <board> [--project dir] [--as name] [--json]
tofu boardy widen <ticket> <glob,glob> [--as name] [--json]
tofu boardy check create|move|widen|close <ticket> [--to status] [--as name] [--json]
tofu boardy tick [--board KEY] [--json]
tofu boardy fire agent.spawn|agent.report|lead.attach [ticket] [--board KEY] [--when before|after] [--as name] [--json]
tofu boardy rule list | show|remove <id> | add|edit <id> [--file hook.yaml] [--when before|after] [--on event] [--every 1h|--daily 09:00] [--do "verb arg"] [--then refuse|nudge|ask|record] [--text t] [--from s] [--to s] [--type t] [--priority p] [--labels a,b] [--actor a] [--epic E|set|empty] [--sprint S|set|empty] [--owns glob] [--in-status-for 2h] | test <id> [ticket] [--to status] [--board KEY] [--scope board|project|user] [--as name] [--json]
tofu boardy bypass list | answer <B> yes|no [--why text] [--board KEY] [--as name] [--json]
tofu boardy request new <words> [--tickets A,B]|add <R> <ticket>...|review <R> pass|fail --note text|show <R>|list [--board KEY] [--as name] [--json]
tofu boardy view [name] [--board KEY] [--epic E] [--label l] [--assignee name] [--as name] [--json]
tofu boardy audit [--board KEY] [--json]
tofu boardy report [--board KEY] [--since YYYY-MM-DD|Nd] [--json]
tofu boardy attention [--board KEY] [--json]
tofu boardy epic list|new <ID> <title> [--milestone M]|research <ID> <note>|design <ID> <design> --first <smallest first version>|milestone <ID> <title> [--due YYYY-MM-DD|Nd]|done <ID> [--board KEY] [--json]
tofu boardy sprint list|new <ID> <title> [--start YYYY-MM-DD|Nd] [--end YYYY-MM-DD|Nd]|start <ID>|close <ID> [--board KEY] [--json]
tofu boardy ask <ticket> --tldr text --eli5 text --options "one | two" --recommend n --why text [--as name] [--json]
tofu boardy catchup [--board KEY] [--answers] [--as name] [--json]
tofu boardy cost <ticket> --claude | --session <id> [--fork N] --claude [--json]
```

`tofu boardy` with no subcommand names every subcommand, and
`tofu boardy <subcommand> --help` prints that subcommand's line. A move
answers with where the ticket is and where it was, such as
`DEMO-1 is doing, was todo`.

See [Boardy](/docs/features/boardy).

### Judgment

```
tofu judge [--dry-run] [--no-cache] [--no-rule] [--json] < request.json
tofu judge --lint file [--json]
tofu check "<command>" [--quiet] [--json]
tofu sift [--arm length|signpost|brevity|jev] [--task text] [--restore] [--no-log] [--json] < text
tofu why <id> | --last [n] [--point name] [--state | --json]
tofu label <id> allow|ask|deny
tofu label --last allow|ask|deny [--json]
tofu replay --point name [--set threshold=value]... [--since 7d] [--verbose] [--json]
tofu calibrate <rule@version> --cases <cases.jsonl> --split <split.json> [--dry-run] [--json]
tofu guard <rule> < hook.json
tofu guard git commit-msg|release-docs <message file>
```

`tofu calibrate` fits a decision point's cut on labelled cases and writes
its lock; `--dry-run` prints the fit and writes nothing. `tofu guard` runs
one of the repository's hook rules; see [Guard rules](/docs/cli/guard).

### Diagnosis

```
tofu doctor [--imports] [--fix] [--json]
tofu version [--json]
tofu update [--check] [--force] [--json]
tofu changelog [--all] [--json]
tofu docs [topic | "a few words"] [--json]
tofu walls [--since <14d|36h>] [--op <op>] [--own] [--tier <S|A|B|C|D|E>] [--all] [--json]
tofu frame [NAME] [--width N] [--height N] [--plain] [--list]
tofu drive [SCRIPT] [--dir PATH] [--home PATH] [--cassette PATH] [--clipboard PATH] [--source NAME] [--quota PATH] [--width N] [--height N] [--timeout 60s] [--plain] [--fresh | --continue] [--host] [--setup] [--live [--model SLUG]] [ARM]
tofu drive desk [--out DIR] [--exe PATH] [--first-run STATE] [STEP...] [-- DESK ARGS]
tofu tool list [--dir <dir>] [--agent <name>] [--session <name|id|head>]
tofu tool <name> <json> [--dir <dir>] [--agent <name>] [--session <name|id|head>] [--sift free|judged]
tofu migrate [--dry-run | --relink] [--json]
```

`tofu walls` prints how long each kind of operation took; see
[Walls](/docs/cli/walls). `tofu drive` runs a script of what a person would
do against the real app, on a cassette unless `--live` answers from a
subscription the home signed in to. `tofu drive desk` opens the desk app in
a home made fresh for the run and keeps a screenshot at each `shot` step and
one at the end; it runs on Windows only.

`tofu tool` lists the tools a run offers and runs one tool call by hand,
printing the exact text the model would receive. See [One tool call by
hand](/docs/cli/tool).

### Frontends

```
tofu serve --stdio [--dir PATH] [--cassette PATH] [--done-review off|cheap|screen|typed]
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
tofu browser [install | uninstall | tabs | build | recipes | open <url> | close <tab id> | bench [--jev] [--n 12] [--rows file] | motion capture <scenario.json>] [--json]
tofu browser observe [--all] | click <ref> | fill <ref> <text> | select <ref> <option> | press <key> | scroll [<ref>] [up|down] | back --tab <id> [--json]
tofu browser read [--tab <id>] [--json]
tofu browser batch [<steps.json> | -] [--tab <id>] [--json]
tofu browser profile [list | new <name> | login <name> [<url>] | stop <name> | remove <name>] [--json]
```
