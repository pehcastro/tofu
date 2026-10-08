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
last lines. A sub-agent that can run bash can always use `shell`.

A bash call with `timeout_ms` of 30,000 or less is the exception: the
command is killed there. Its result and the session trace say `bash: hit
the deadline after 3 s`, and its killed row on the shells screen ends
`tofu: hit the deadline after 3 s and was killed`.

Every bash command is on the shells screen from its first second, not
only once it moves at 30 s; one that ends inside the 30 s leaves the
screen when it ends. While a turn runs, the chat shows the last line of
each running command under the working line:

    [&orchestrator] running bash, 3s  |  fg 3

Each row shows the command, who started it, its latest output and, once
it ends, its exit code. The screen reads it four times a second while any
shell runs, turn or no turn. A line reaches the log as the command writes
it; a running shell that printed nothing says `nothing printed yet`.
Python is told not to buffer its output.

When the command names its own log file, such as `-Log target/build.log`,
`> build.log` or `tee build.log`, the row also shows the end of that file,
so a build that prints nothing to the console is still visible. The path
is read from where the command is when it writes it, so
`cd apps/desk && ... -Log target/build.log` reads
`apps/desk/target/build.log`. Only a path written in the command counts,
and a file older than the shell, left by an earlier run, is not shown.

A pipeline ending in `| grep`, `| sed`, `| awk`, `| cut`, `| tr` or
`| uniq` runs in a pseudo terminal (ConPTY on Windows), since those print
in blocks to a pipe: a loop printing once a second into `| grep` shows
its lines at 0.2, 1.2, 2.2, 3.3 and 4.4 s, against all five at 5.6 s on a
pipe. Every other command stays on a pipe, because a terminal costs about
a quarter of a millisecond a line and makes some tools draw progress
bars. Exit code and log are the same either way; `tofu shells --json`
says `"terminal": true` for one that ran in a terminal.

Some commands still hold their output back, and the row's first line says
so: `| tail -2`, `| sort` and `| wc` print when the command ends, `| head`
when it has its lines, and a mid-pipeline filter, such as the `grep` in
`| grep x | sed y`, in blocks:

    tofu: piped into tail -2, which prints when the command ends

## Where it lives

Shells belong to the running app and the session that started them. When
`persistentRegistry` is on, the list is kept in the project's state folder
and survives a restart of the app; when it is off, closing the app stops
every shell it started, with the processes each one started.

A shell's output is a file in the project's state folder, and the screen
shows its last `logTail` lines, read from the last 256 KiB. A log file
the command names stays wherever the command wrote it.

## Change it

Stop a shell and every process it started:

    tofu shells stop bash-3

Keep shells across a restart of the app:

    tofu settings set persistentRegistry true

Keep more lines of each shell's output:

    tofu settings set logTail 2000

Ask before stopping a shell from the shells screen:

    tofu settings set killConfirm true

## Check it

List every shell, who started it, whether it still runs, how long it ran
and when it last printed. A finished one stays listed for 72 hours, and a
new shell is numbered past every name listed, so a name means one command:

    tofu shells list

Print a shell's last lines, the same text `shell logs` gives the model,
which also says `ran 1m 1s, last output 2s ago`:

    tofu shells log bash-3

A build or test that can move to a background shell runs without rtk,
which prints nothing until exit; the call's proxy note says so.

In the app, alt+4 opens the shells screen. `tofu session trace` shows the
bash call that moved a command to a shell, with the shell's name in its
result.

## Undo it

A shell that should not keep running is stopped with `tofu shells stop`.
Turn `persistentRegistry` back off with:

    tofu settings set persistentRegistry false

A model that gave up waiting on a shell leaves it running and listed
until it ends or is stopped.
