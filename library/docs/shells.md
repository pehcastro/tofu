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
command is killed at that limit, as before.

Each row on the shells screen shows the command, who started it, its
latest output and, once it ends, its exit code. When the command names its
own log file, such as `-Log target/build.log`, `> build.log` or
`tee build.log`, the row also shows the end of that file, so a build that
prints nothing to the console is still visible. Only a path written in the
command counts.

## Where it lives

Shells belong to the running app and the session that started them. When
`persistentRegistry` is on, the list is kept in the project's state folder
and survives a restart of the app; when it is off, closing the app stops
every shell it started, with the processes each one started.

A shell's output is held in memory up to `logTail` lines. A log file the
command names stays wherever the command wrote it.

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

List every shell, who started it and whether it still runs:

    tofu shells list

Print a shell's last lines:

    tofu shells log bash-3

In the app, alt+4 opens the shells screen. `tofu session trace` shows the
bash call that moved a command to a shell, with the shell's name in its
result.

## Undo it

A shell that should not keep running is stopped with `tofu shells stop`.
Turn `persistentRegistry` back off with:

    tofu settings set persistentRegistry false

A model that waited on a shell and gave up leaves it running; the shells
screen still lists it until it ends or is stopped.
