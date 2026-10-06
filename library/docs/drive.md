---
topic: drive
title: Driving the app from a script
summary: what tofu drive does, the steps a script takes, and what the requests step prints
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
  settings under that home instead.

The run prints the settings it resolved before the first step, so a pasted
run carries the conditions it was taken under.

## Change it

The steps a script takes, among others:

- `type TEXT`, `paste TEXT`, `key NAME`: what a person types and presses
- `wait TEXT`, `absent TEXT`: what must or must not be on the screen
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

## Check it

    tofu drive steps.drive --cassette read.cassette --plain

with `steps.drive` holding:

    type read note.txt
    key enter
    wait cooked for
    requests text 2

prints the settings, then request 2 with every message and its text.
`tofu drive --help` prints every step, every flag, and the cassette's
fields.

## Undo it

A run without `--home` writes nothing of yours: its home is deleted when
it ends. A run with `--home` writes its session under that home, so delete
the session there, or point `--home` at a folder made for the run.
