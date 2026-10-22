---
topic: shells
title: Shells and long commands
summary: why a long build moves to a background shell, how the model reads it back, and what its row shows
verbs: shells
---

## What it is

A shell is a process tofu keeps running after the bash call that started
it returned. There are two kinds: a server or watcher the model started
with `background: true`, and a command that was still running 30 s after
its bash call started, such as a long build or test run.

The second kind is not killed. The call returns the shell's name, such as
`bash-3`, and what it printed so far, and the command keeps running. The
model, or a sub-agent, reads it back with `shell wait`, which returns as
soon as the command ends, or after another 30 s, with its exit code and its
last lines. The lead's own command moves after 10 s instead, or at once when you type, and its exit comes back to the lead as one message, so the lead never waits on it and you can always talk to it. A sub-agent that can run bash can always use `shell`, and may
start a server, a watcher or a `sleep` with `background: true`; outside
the background it sleeps 5 s at most.

A bash call with `timeout_ms` of 30,000 or less is the exception: the
command is killed there, its result and trace say `bash: hit the deadline
after 3 s`, and nothing of it stays on the shells screen or on disk. A bash
call may carry a `title`, a few words saying what the command is for.

A one-shot command, such as a test or a version check that ends inside
its wait, is never a shell: neither the shells screen nor `tofu shells`
lists it, and a background start that ends inside its 10 s says it has
no shell name to wait on. A name is never given to a second command, so
a name in a sub-agent's report is the one the orchestrator waits on,
reads and stops, and the screen and `tofu shells` show that same name.
While a turn runs, the chat shows the last line of each running command
under the working line:

    [&orchestrator] running bash, 3s  |  fg 3

While the lead waits on its sub-agents, the ones in bash are counted on the
status line above the composer rather than on a line of their own:

    waiting  |  5 sub-agents · 1 in bash  |  6m 1s

Each row is a running shell: the command, who started it and its latest output. A shell that ends leaves the screen at once and tofu stops reading it;
its record and log stay on disk for 72 hours, read by name with `tofu shells log`. The screen reads the running shells four times a second while any runs. A line reaches the log as the command writes
it; a running shell that printed nothing says `nothing printed yet`. Python is told not to buffer its output.

When the command names its own log file, such as `-Log target/build.log`,
`> build.log` or `tee build.log`, the row also shows the end of that file,
so a build silent on the console is still visible. The file stays where
the command wrote it, and its path is read from where the command is when it writes it, so
`cd apps/desk && ... -Log target/build.log` reads
`apps/desk/target/build.log`. Only a path written in the command counts,
and a file older than the shell, left by an earlier run, is not shown.

A pipeline ending in `| grep`, `| sed`, `| awk`, `| cut`, `| tr` or
`| uniq` runs in a pseudo terminal (ConPTY on Windows), since those print
in blocks to a pipe: a loop printing once a second into `| grep` shows
its lines at 0.2, 1.2, 2.2, 3.3 and 4.4 s, against all five at 5.6 s on a
pipe. Every other command stays on a pipe, because a terminal costs about
a quarter of a millisecond a line and makes some tools draw progress
bars. Exit code and log are the same either way.

A person can take a kept shell over from a client that declares `takeover` to `tofu serve`, such as the desk: from then on every bash command runs in a terminal, foreground or background, and Take over on a running shell hands its keys to the person, with what it printed so far, colours included. Taking over and releasing never kill it, and a held shell that ends goes back to the agent and leaves the list.
While the person holds it the agent backs off: its `shell` logs, wait, stop and restart answer `bash-3 is held by the person, who took it over at 21:03:11 ...`, a bash kill of it and a sub-agent kill leave it running, and the agent is told at take-over and again at release, with the person's note.
The TUI and `tofu run` declare nothing, so their shells stay on pipes and a foreground `read x` ends at once, as before.

Some commands still hold their output back, and the row's first line says
so: `| tail -2`, `| sort` and `| wc` print when the command ends, `| head`
when it has its lines, and a mid-pipeline filter, such as the `grep` in
`| grep x | sed y`, in blocks:

    tofu: piped into tail -2, which prints when the command ends

## Where it lives

Shells belong to the running app and the session that started them. When
`persistentRegistry` is on, the list is kept in the project's state folder
and survives a restart of the app; when it is off, closing the app stops
every shell it started, with the processes each one started. A shell's
output is a file there too, and the screen shows its last `logTail`
lines, read from the last 256 KiB.

## Change it

Stop a shell and every process it started, or keep shells across a
restart, keep more lines of output, or ask before a stop from the screen:

    tofu shells stop bash-3
    tofu settings set persistentRegistry true
    tofu settings set logTail 2000
    tofu settings set killConfirm true

## Check it

List every running shell, who started it, whether it is `running, held by a person` or `left over` from a tofu that closed, how long it
has run and when it last printed. An ended shell is not listed:

    tofu shells list

With `--json` each also says why it was kept, `"kept": "background"` or `"moved"`, its folder as `dir`, the port its command names as `port`,
what ended the wait as `ready` (`port`, `line` for a ready line, or `waited`), `"terminal": true` for one run in a terminal
and `"holder": "person"` and `held_since` while a person holds it.

Print a shell's last lines, the same text `shell logs` gives the model,
which also says `ran 1m 1s, last output 2s ago`. It reads an ended shell by name too, for 72 hours:

    tofu shells log bash-3

A build or test that can move to a background shell runs without rtk,
which prints nothing until exit; the call's proxy note says so. In the
app, alt+4 opens the shells screen. `tofu session trace` shows the bash
call that moved a command to a shell, with the shell's name in its result.

## Undo it

Stop a shell with `tofu shells stop`, and turn `persistentRegistry` off
with `tofu settings set persistentRegistry false`. A model that gave up
waiting on a shell leaves it running and listed until it ends or is stopped.
