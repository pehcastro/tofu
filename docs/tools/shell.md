---
title: Shell
description: bash wraps your shell for one command at a time, and shell is tofu's own registry of the servers bash kept running.
order: 3
updated: 2026-10-09
---

`bash` is a wrapper: it runs `<your shell> -c <command>` in the working
directory with your environment, and returns stdout and stderr merged. `shell`
is tofu's own Go registry of the processes `bash` started in the background.

The environment keeps your variables, with three changes:
- tofu's own keys are taken out, so `OPENROUTER_KEY` never reaches a command the model runs.
- `AI_AGENT=tofu` is set, so tools such as vitest print their short agent report. One passing run went from 737 bytes to 240.
- git never waits on a prompt. `GIT_EDITOR` is `true`, and `GIT_TERMINAL_PROMPT=0` and `GCM_INTERACTIVE=never` are set, so a `git commit` with no message returns at once with git's own error.

On Windows, output a native tool prints in the console's code page, such as
437 or 850, is turned into UTF-8 before the model reads it, so `Página` stays
`Página`. PowerShell runs with `-NoProfile` and UTF-8 output. A command that
switches the console to another code page, as some installers do, does not
outlast tofu: when tofu exits, the console gets back the code page it had
when tofu started, so `cmd` or PowerShell after it prints as before.

On Windows the shell is Git Bash, then a `bash` on `PATH` that isn't WSL, then
PowerShell. Elsewhere it's `$SHELL`, or `/bin/sh`. The `shell` setting or
`TOFU_SHELL` overrides it.

## Limits, guards and background servers

A command still running after 30 s, such as a long build, moves to a
background shell instead of holding the call. The call returns its name, such
as `bash-3`, and what it printed so far, and the command keeps running on the
**shells** tab. The model reads it back with `shell wait`, which returns when
the command ends or after another 30 s, with its exit code and last lines.
Nothing is killed at a deadline, so a 10 minute build is not lost at 10
minutes. A `timeout_ms` of 30,000 or less is a hard limit instead: the
command is killed there and returns what it printed. The result and the
session trace say `bash: hit the deadline after 3 s`, and the killed row
stays on the **shells** tab ending `tofu: hit the deadline after 3 s and
was killed`.

The **shells** tab lists only the shells tofu keeps: a server or watcher
started with `background: true`, and a command that moved to the
background after 30 s. A one-shot command, such as a test or a version
check that ends inside its wait, is never a shell, so the tab and `tofu
shells` stay a list of what is still yours to watch or stop. While a turn
runs, the chat shows each running command's last line under the working
line. The tab reads four times a second while any shell runs, turn or no
turn, and a kept shell keeps one name for good, so `bash-3` in a
sub-agent's report is the one the tab shows. `shell wait` and `shell logs` say how long it
ran and when it last printed, such as `ran 1m 1s, last output 2s ago`.

When a command names its own log file, such as `-Log target/build.log`,
`> build.log` or `tee build.log`, its row on the **shells** tab and
`shell logs` also show the end of that file, so a build that prints nothing
to the console is still visible. The path is read from where the command
is, so `cd apps/desk && ... -Log target/build.log` reads
`apps/desk/target/build.log`, and a file older than the shell is skipped.
Only a path written in the command counts, not one a script computes
inside itself.

A command whose pipeline ends in `grep`, `sed`, `awk`, `cut`, `tr` or
`uniq` runs in a pseudo terminal (ConPTY on Windows), so the filter prints
each line as it comes instead of in blocks: a loop printing once a second
into `| grep` shows its lines at 0.2, 1.2, 2.2, 3.3 and 4.4 s, where on a
pipe all five arrived at 5.6 s. Every other command stays on a pipe,
because a terminal costs about a quarter of a millisecond a line and makes
some tools draw progress bars. `tofu shells --json` says
`"terminal": true` for a command that ran in one.

A command that still holds its own output says so in its first line
instead of showing an empty row: `tofu: piped into tail -2, which prints
when the command ends`, and the same for `head`, `sort`, `wc`, and a
`grep`, `sed` or `awk` in the middle of a pipeline. Python is told not to
buffer its output.

A command that prints a lot keeps its first and last 32 KiB while
it runs, and says how many bytes it dropped from the middle. tofu probes the project's interpreters once, in
the background, and refuses a command that names one missing from `PATH`
rather than letting it fail as `not found`. The probe used to block: building
the tool in a Node project took 780.7 ms, and now 0.5 ms.

Long-running servers need their own owner. `background: true` returns as soon
as the port opens or a ready line prints, 151 ms in one run where tofu used to
wait a fixed 10 s, and keeps the process on the **shells** tab after the turn.
A start on a port another process holds is refused with a free port. A `kill`
of a process tofu started runs as `shell stop`, which ends the whole process
tree, so the port is free.

When a classifier is set up, long `bash` output is sifted before the model
reads it.

## Shorter output through rtk

By default, each `bash` command is first passed to `rtk rewrite`, with a 5 s
limit, and the rewritten command runs instead, such as `rtk git diff` for
`git diff`, so the model reads a compact form of the same output. A
background command, a piped one and a redirected one run as written, and
so does a build, test or install that could move to a background shell,
because rtk prints nothing until the command exits. rtk
also stands aside when it isn't on `PATH`, when the project ships
`.rtk/filters.toml`, and when the rewrite is `rtk tsc` or `rtk npx`, which
can download a compiler.

To turn it off, put this in `tools/shell/proxy.yaml` under `~/.tofu` or the
project's `.tofu`:

```yaml proxy.yaml
use: off
```

## Watching and stopping servers

The model runs commands. You watch kept processes on the **shells** tab, and
stop them there or from the command line.

| Tool | Parameter | What it does |
|---|---|---|
| `bash` | `command` | the command to run |
| `bash` | `timeout_ms` | a hard limit, only when 30,000 or less |
| `bash` | `background` | keep a server or watcher alive |
| `bash` | `check_port` | who holds a port, without an HTTP request |
| `shell` | `op` | `wait` until it ends, `logs` for the last 200 lines, `stop`, or `restart` |
| `shell` | `name` | the name `bash` returned, such as `bash-1` |

## Commands

```bash
tofu shells list
```

```text
Shells                                                         ○ none registered
```

`tofu shells log|stop|restart <name>` act on one process, and every
subcommand takes `--json`. A finished shell stays on the list for 72 hours.
A new shell is numbered past every name still listed, so `bash-3` names one
command, also across `tofu --continue`. With `--json`, each shell says why
it was kept, `"kept": "background"` or `"moved"`, its folder, the port its
command names, and its exit code once it ends. Each kept shell is also a
[program status](/docs/features/status) record, `shells/<name>`, so a
terminal that reads OSC 7501 shows it working, done or failed.
