---
topic: doctor
title: The doctor
summary: what tofu doctor checks, what each line it prints means, and what to do about it
verbs: doctor, library, usage, version, frame, drive, lint, sift, browser
---

## What it is

`tofu doctor` says whether tofu can run in this directory, and what is
wrong when it cannot. It changes no setting and no file of yours. The
first line names the version, Go and the system, and ends `✓ ready` or
`✗ not ready`; it exits 0 when ready and 1 when not.

## Where it lives

The doctor reads what tofu would read to start: your credentials in
`~/.tofu/agent.db`, the OpenRouter key, the decision points tofu ships or
a `library/` folder in the working directory, and this project's ledger
under `~/.tofu/projects/`. It writes one thing, the quota reading it takes
from each subscription, under `~/.tofu/quota/`.

## Change it

A `✓` line needs nothing, a `⚠` line needs a look, and a `✗` line stops
tofu. A line with `→` names the command that fixes it.

What stops tofu, printed first:

    ✗ claude-sub  no subscription is signed in, so no model can answer
      → tofu login claude-sub
    ✗ jev  there is no openrouter key, so jev judges no tool call
      → tofu login openrouter

`tofu login codex-sub` also clears the first. `tofu docs gate` says more
about the second.

Under `access`, what tofu can reach:

- `✓ claude-sub  5h 12% · 7d 40%`: the share of each quota window used.
- `⚠ ... every window is spent, back at <time>`: wait, or sign in the
  other subscription.
- `⚠ ... the credential is broken, run tofu login <source>`: run it.
- `⚠ ... signed in, no window reported`: the subscription answered
  without a quota reading, so tofu cannot say how much is left.
- `⚠ ... not polled, ...`: the credential cannot be used, and the rest of
  the line says why and what to run.
- `✓ jev  key · credential store`: where the key was found. The key is
  never printed.
- one line per wire, ending `subscription` or `money`: what a model call
  that way spends.

Under `shell`, one line about rtk. tofu asks rtk to rewrite every bash
command before it runs, so `git diff` runs as `rtk git diff` and prints
less. The bash row keeps the command asked for and the one that ran.

- `✓ rtk  <version> · rewrites every bash command`: rtk is on PATH.
- `⚠ rtk  not on PATH, ...`: every command runs as asked and nothing
  fails. The `→` names the install command for this system.
- `○ rtk  off`: a layer turned it off, and the line names which.

To turn it off, put one line, `use: off`, in `tools/shell/proxy.yaml`
under `~/.tofu/` for every project, or under `.tofu/` in a project for
that project only. Delete the file to turn it back on.

Under `browser`, one line for each browser tofu can share tabs from: Chrome
on Windows, and on Linux and macOS Chrome, Chromium, and Brave or Edge when
their profile folder is there. The line is about the native host, the file
the browser reads to start tofu.

- `✓ <browser>  native host installed`: the browser can reach tofu.
- `⚠ <browser>  no native host, ...`: the browser cannot start tofu, so its
  tabs stay out of reach. The `→` names `tofu browser install`.
- `⚠ <browser>  the native host names a tofu that is gone`: tofu moved or
  was deleted since the install. Run `tofu browser install` again.

Under `rules`, what tofu decides with:

- `library  binary · <n> points` is normal. `project · <n> of <m> points`
  means a `library/` folder here replaces some decision points.
- jev's decision points, not the rules `tofu docs rules` describes, grouped
  by mode, `○ shadow` or `● enforced`, and where the thresholds come from.
  A `⚠` point runs in another mode than its file asks, and says why. A `✗`
  point is `unusable` and says what is wrong; fix that file or delete it.

Under `state`, `calibration` names the points with a calibration lock, or
`none`, and `ledger` how many decisions are recorded here, over how many
days, with any unreadable lines.

## Check it

    tofu doctor
    tofu doctor --json

The second prints one JSON document, `{tofu, verb, ok, at, data,
problems}`, with every field the first folds away and each blocker in
`problems`. Every verb below takes `--json` the same way, except `frame`
and `drive`, which print screens.

Other verbs that each check one thing:

- `tofu library` checks every library file tofu reads, and these pages,
  and exits 1 when one is refused. `tofu library resolve <name>` shows
  where each field of one entry came from.
- `tofu usage` prints each credential's quota windows and when they
  reset. `--history` prints the readings already taken.
- `tofu version` prints the version, and `tofu changelog` what changed.
- `tofu frame --list` names the app's recorded screens, and
  `tofu frame <name> --width 120 --height 40` prints one, to see how the
  app draws in your terminal.
- `tofu drive` runs a script of what a person would do in the app, with no
  terminal and no model call, and prints the screens it asks for.
- `tofu lint comments <path>` lists every comment in the Go source under
  a path.
- `tofu sift` marks which paragraphs of standard input are worth reading.
- `tofu browser` lists the Chrome tabs tofu can reach.
  `tofu browser install` sets up the extension and its native host.

## Undo it

The doctor changes nothing of yours, so there is nothing to undo. It
does keep each quota reading it takes, for `tofu usage --history`; delete
`~/.tofu/quota/` to forget them.
