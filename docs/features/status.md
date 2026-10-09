---
title: Program status
description: tofu tells your terminal whether the lead, each sub-agent, each kept shell and each cron job is working, waiting on you, done or failed, through the OSC 7501 Program Status Protocol.
order: 17
updated: 2026-10-09
---

While you work in another tab, tofu tells the terminal it runs in what it is
doing: working, waiting on you, done, or failed, and why. A terminal that
understands it shows it on the tab, in its session list or in an inbox, so
you know a turn finished or needs an answer without switching back to look.

## The standard it follows

tofu speaks the [Program Status Protocol, OSC
7501](https://www.superlogical.com/rex/docs/build/program-status), a
terminal escape sequence by Mitchell Hashimoto that lets any program report
its state to the terminal in a few `key=value` pairs. His [post on the
protocol](https://mitchellh.com/writing/program-status-osc7501) explains why
it exists: tools that watch many coding agents at once guess their state by
matching window titles and screen text, and each guess breaks when an agent
changes its spinner. The protocol lets the program that knows its state say
it, over the terminal it already writes to, which also works over SSH and
inside a container.

tofu implements it as the spec describes. A report is `ESC ] 7501 ;`, then
`state` and optional `id`, `kind`, `progress`, `app`, `title` and `msg`,
then `ESC \`. Titles and messages are base64 UTF-8, with any control
character sent as a space, and every limit in the spec is kept: an id
segment stops at 32 bytes, a title at 192 and a message at 2048. Where tofu
goes further than a single program:

- **One record per thing that runs.** The lead is the root record, with
  `app=tofu` and the session's name as title. Each sub-agent is
  `agents/<name>`, its own sub-agents sit under it, such as
  `agents/go-dev-1/agents/research-1`, each kept shell is `shells/<name>`,
  and a running cron job is `cron/<id>`.
- **A program inside a kept shell speaks for that shell.** A nested tool
  that reports OSC 7501 itself, such as a `claude` waiting on a login,
  makes its shell `blocked:kind=auth`, and its own records move under
  `shells/<name>/`.
- **Records are cleaned up.** Quitting tofu sends `state=clear`, which
  removes every record it sent.
- **The same records reach other programs.** `tofu serve` sends each one as
  a `status` event, so the desk app or a script reads the same picture
  without a terminal.

tofu doesn't send the spec's optional detection query. It always reports,
because a terminal that doesn't know the sequence ignores it: Windows
Terminal and the legacy Windows console both drop it and print nothing.

## What each record says

The lead is `working` while a turn runs, `blocked:kind=permission` while an
approval waits for you, `blocked:kind=question` while an `ask_person` form
waits, `error` when the turn failed, and `idle` at the prompt or after you
stop it. A turn that ends while the terminal is not focused leaves `done`,
which turns `idle` once you focus the terminal, press a key or click.

| Record | States |
|---|---|
| the lead | `idle`, `working`, `blocked` (permission or question), `done`, `error` |
| `agents/<name>` | `working`; `blocked:kind=question` while it waits on the lead; `blocked:kind=permission` while one of its calls waits on you; `done`; `idle` when parked; `error` |
| `shells/<name>` | `working`, then `done` on exit code 0 or `error` with the code |
| `cron/<id>` | the lead's state while the job's turn runs, until you have seen it |

A sub-agent's or a shell's `done` and `error` clear once you open the
**sub-agents** or **shells** tab. A record's message is what it is doing
now: a sub-agent's current step, or a shell's command.

## Seeing it

There is nothing to turn on. Run tofu in a terminal that implements OSC
7501, such as Rex or one built on libghostty, start a turn, and the tab
shows it working, then done. To change how it is shown, or to hide it, use
the terminal's own settings.

To read the bytes, run tofu under a program that records its terminal
output and look for `ESC ] 7501 ;`. A turn reads, in order, with each title
in base64:

```text
state=idle:app=tofu
state=working:app=tofu:title=...
state=done:app=tofu:title=...
```

A program in a kept shell can report for itself. On Git Bash for Windows, a
report ended by `ESC \` is dropped before tofu sees it, so end it with
`BEL`:

```sh
printf '\e]7501;state=working:progress=40\a'
```

If tofu was killed before it could clear its records, clear them from the
same terminal:

```sh
printf '\e]7501;state=clear\a'
```

Over `tofu serve --stdio`, every record arrives as a `status` event with
`id`, `state` and the optional keys, and `status.list` answers every record
as it stands, for a client that connects late. See [Serving tofu to another
program](/docs/cli/serve).
