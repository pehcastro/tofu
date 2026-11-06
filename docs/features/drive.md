---
title: Driving
description: Agents drive the terminal app and the desk app the way a person does, in a home made for the run, and keep what they saw.
order: 22
updated: 2026-11-02
---

A claim that a screen works is worth what the person can check. tofu lets
an agent drive the real app, the terminal interface or the desk, in a home
made fresh for that run, and keep the screens it saw, so a ticket carries a
screenshot and a step log instead of a sentence.

## How it works

- `tofu drive` runs the terminal app from a script of steps, with no
  terminal and no model call: the model is a cassette of recorded replies.
- A terminal script waits with `idle` until no turn runs and no sub-agent
  works, and `count TEXT` prints how many screen rows hold TEXT, so a run
  proves a message shows once. The run ends naming the session it recorded
  and the `tofu session trace` command that reads it.
- `tofu drive desk` opens the desk app, takes its steps, and writes a
  screenshot once the window has painted, one at each `shot` step and one at
  the end, with a step log beside them. It prints the folder they are in and
  closes the desk.
- Neither run touches your home. `tofu drive desk` points `HOME`,
  `USERPROFILE`, `APPDATA`, `LOCALAPPDATA` and the XDG folders at a `home`
  folder inside its output folder, so the desk it opens reads and writes
  only there, and your own desk keeps running untouched.
- The desk opens on a Windows desktop made for the run, not the one you
  see, so its window never shows on your screen or taskbar. Clicks and keys
  go to that window as messages, and the screenshots come from it. The
  real cursor and keyboard stay yours while it runs.
- It builds nothing. It runs the newest `tofu-desktop` build under
  `CARGO_TARGET_DIR` or `apps/desk/target*`, or the one `--exe` names.
- A sub-agent may run `tofu drive desk` from its shell even when it holds no
  path in the project: the screenshots go to a new folder under its own
  scratch `tmp`, or to `--out` inside a path it holds. `--exe` is refused to
  a sub-agent, so it drives the build the project already has.

## Steps

| Step | What it does |
| --- | --- |
| `click:X,Y` | click at X,Y in the window, in pixels from its top left |
| `type:TEXT` | type TEXT into what has focus |
| `key:NAME` | press enter, tab, escape, backspace, delete, space, an arrow, home, end, pageup, pagedown, or f1 to f12 |
| `wait:DUR` | wait `500ms`, `2s`, or any duration |
| `shot:NAME` | write a screenshot named NAME |

`--first-run ready` opens a fresh home past onboarding, and so do
`no-tofu`, `no-account` and `no-project` for the screen each names.
Everything after `--` goes to the desk as it is: `-- --screen boardy` opens
that screen, and `-- FOLDER` opens the desk on a project.

## Commands

| Command | What it does |
| --- | --- |
| `tofu drive desk --first-run ready click:20,20 -- demo` | open the desk on the project `demo`, click the sidebar toggle, keep two screenshots |
| `tofu drive desk --out shots click:20,20 shot:closed click:20,20` | open, toggle the sidebar twice, keep three screenshots in `shots`, which must be empty or absent |
| `tofu drive desk -- --screen boardy` | open the desk on the board screen and keep its screenshots |
| `tofu drive desk --help` | every step and flag |
| `tofu drive steps.drive --cassette read.cassette --plain` | drive the terminal app from a script |

A run prints:

```
desk    apps\desk\target_dev\motion\tofu-desktop.exe, built 2026-10-30 21:58:50
home    shots\home
shot    shots\01-open.png
shot    shots\02-end.png
log     shots\steps.log
closed  the desk quit on close
folder  shots
```

## Limits

`tofu drive desk` drives the desk on Windows. On Linux and macOS it says so
and exits with 2.
