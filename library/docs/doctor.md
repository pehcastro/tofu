---
topic: doctor
title: The doctor
summary: what tofu doctor checks, what each line it prints means, and what to do about it
verbs: doctor, library, usage, version, frame, drive, lint, sift, browser
---

## What it is

`tofu doctor` says whether tofu can run in this directory, and what is
wrong when it cannot. It changes no setting and no file of yours. The
first line is the version and `ready` or `not ready`; it exits 0 when
ready and 1 when not.

## Where it lives

The doctor reads what tofu would read to start: your credentials in
`~/.tofu/agent.db`, the OpenRouter key, the decision points tofu ships or
a `library/` folder in the working directory, and this project's ledger
under `~/.tofu/projects/`. It writes one thing, the quota reading it takes
from each subscription, under `~/.tofu/quota/`.

## Change it

Every line below says what to run. The lines come in groups.

What stops tofu, printed first, each with the command that fixes it:

- `claude-sub  no subscription is signed in`: run `tofu login claude-sub`,
  or `tofu login codex-sub`.
- `jev  there is no openrouter key`: run `tofu login openrouter`. Until
  then no tool call is judged. `tofu docs gate` says more.

What tofu can reach:

- One line per stored credential. `5h 12%  7d 40%` is the share of each
  quota window used, and nothing needs doing.
- `every window is spent, back at <time>`: wait, or sign in the other
  subscription.
- `the credential is broken, run tofu login <source>`: run it.
- `signed in, no window reported`: the subscription answered without a
  quota reading, so tofu cannot say how much is left.
- `not polled, ...`: the credential cannot be used, and the rest of the
  line says why and what to run.
- `jev  key from ...`: where the key was found. The key is never printed.
- `wires`: which ways of reaching a model spend subscription quota, and
  which spend money.

What tofu decides with:

- `library`: `the one in the binary` is normal. `the project's own` means
  a `library/` folder here replaces some decision points.
- `rules`: jev's decision points, not the rules `tofu docs rules`
  describes, each with its mode, `shadow` or `enforced`, and where its
  thresholds come from. A point whose file asks for one mode and runs in
  the other says why. A point `unusable` says what is wrong with its file;
  fix that file or delete it.
- `calibration`: the points with a calibration lock, or `none`.
- `ledger`: how many decisions are recorded here, over how many days.
  `unreadable lines` counts lines of the ledger that could not be read.

## Check it

    tofu doctor
    tofu doctor --json

The second prints every field, including ones the first folds away.

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
- `tofu browser` lists the Chrome tabs you shared with tofu.
  `tofu browser install` sets up the extension's native host.

## Undo it

The doctor changes nothing of yours, so there is nothing to undo. It
does keep each quota reading it takes, for `tofu usage --history`; delete
`~/.tofu/quota/` to forget them.
