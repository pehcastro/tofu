---
topic: drive
title: Driving the app from a script
summary: what tofu drive and tofu drive desk do, the steps they take, and what the requests step prints
verbs: drive
---

## What it is

`tofu drive` runs the app the way a person does, with no terminal and no
model call. A script says what to type, which keys to press, what to wait
for, and when to print the screen. The model is a cassette: one recorded
reply per line of JSON, handed out in order, so two runs of one script
answer the same.

## Where it lives

- the script: a file of steps, one per line, or `-` for standard input
- the cassette: `--cassette PATH`, or `TOFU_DRIVE_CASSETTE` when the flag
  is not given. Without one no wire opens and a turn fails saying so.
- the home: without `--home` the run makes an empty one, reads no settings
  file of yours, and deletes it at the end. `--home PATH` reads the
  settings under that home instead. Your real home, a folder above it,
  its `.tofu`, or any folder under it outside the temp folder is refused
  with exit 2 before anything there is opened. A folder inside the temp
  folder always drives: one made by `mktemp -d` does even where the temp
  folder sits inside your profile, and so does one that `HOME` points at.

The run prints the settings it resolved before the first step, so a pasted
run carries the conditions it was taken under.

`TOFU_DRIVE_CLOCK=2026-10-17T23:59:58` starts the session's clock at that
local time and runs it on from there, so a run crosses midnight in seconds.
It moves the date the model is told and the cron book, and is read only
beside a cassette, in the drive and in the host it starts.

## Change it

The steps a script takes, among others:

- `type TEXT`, `paste TEXT`, `key NAME`: what a person types and presses
- `wait TEXT`, `absent TEXT`: what must or must not be on the screen
- `count TEXT`: print how many rows of the screen hold TEXT, as
  `count TEXT: N`, so a run proves a message shows once rather than only
  that it shows; `count` with no text is refused; `idle` waits until no turn runs and no sub-agent works
- `screen`: print the screen as it stands
- `environment`: print the environment block the last turn sent
- `requests`: print every request sent so far
- `requests text`, `requests text N`: the same, with the text of each
  message; `N` prints only the Nth request sent

`requests` prints one summary line per request, with its bytes, its tool
count and a hash of its tools, and under it one line per message:

    the orchestrator request 2: 38408 bytes, 20 tools f9570722f298
      message 1 system: 14676 bytes
      message 2 user: 271 bytes
      message 3 assistant: 0 bytes, calls read cassette_1_1
      message 4 tool: 21 bytes, result of cassette_1_1

The role, the bytes of the message's text, the id of every tool call an
assistant message makes, and the id of the call a tool message answers.
`requests text` prints each message's text and tool calls under its line,
each line cut at `--width`, and every key masked as the session log masks
it, so a pasted run never carries one.

## The desk

`tofu drive desk` opens the desk app in a home made fresh for the run,
takes its steps, and writes a screenshot once the window has painted, one
at each `shot` step and one at the end, with `steps.log` beside them. It
prints the folder and closes the desk. `HOME`, `USERPROFILE`, `APPDATA`,
`LOCALAPPDATA` and the XDG folders point at `home` inside that folder, so it
never reads or writes your home or your running desk. It builds nothing:
it runs the newest `tofu-desktop` build under `CARGO_TARGET_DIR` or
`apps/desk/target*`, or the one `--exe` names.

- `click:X,Y`: click at X,Y in the window, in pixels from its top left
- `type:TEXT`, `key:NAME`: type into what has focus, press a key
- `wait:DUR`: wait `500ms`, `2s`, any duration
- `shot:NAME`: write a screenshot named NAME
- `--out DIR`: write there, empty or absent; without it a new folder
  under the temp folder
- `--first-run ready`: open a fresh home past onboarding; also
  `no-tofu`, `no-account`, `no-project`
- after `--`: arguments for the desk, `-- --screen boardy` for a screen or
  `-- FOLDER` for a project

The desk opens on a Windows desktop made for the run, so its window never
shows on your screen or taskbar, and clicks and keys go to it as messages,
never through the real cursor or keyboard. A sub-agent runs it from its shell without holding a
path, writing under its scratch `tmp` or into `--out` inside a path it
holds; `--exe` is refused to it. Windows only for now.

    tofu drive desk --first-run ready click:20,20 -- demo

opens the desk on the project `demo`, clicks the sidebar toggle, and keeps
`01-open.png` and `02-end.png`.

## Check it

    tofu drive steps.drive --cassette read.cassette --plain

with `steps.drive` holding:

    type read note.txt
    key enter
    wait cooked for
    requests text 2

prints the settings, then request 2 with every message and its text, and ends naming the session it recorded and the `tofu session trace` command that reads it.
`tofu drive --help` prints every step, every flag, and the cassette's
fields.

## Undo it

A run without `--home` writes nothing of yours: its home is deleted when
it ends. A run with `--home` writes its session under that home, so delete
the session there, or point `--home` at a folder made for the run. It
never writes your real home: `--home` refuses it.
