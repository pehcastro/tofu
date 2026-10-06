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

## The shells screen

Each row shows the command, who started it, its latest output and, once it
ends, its exit code. When the command names its own log file, such as
`-Log target/build.log`, `> build.log` or `tee build.log`, the row also
shows the end of that file, so a build that prints nothing to the console
is still visible. Only a path written in the command counts.

## Commands

- `tofu shells list` lists every shell and its state.
- `tofu shells log <name>` prints a shell's last lines.
- `tofu shells stop <name>` kills a shell and every process it started.
