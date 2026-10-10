---
title: Changelog
description: What changed for you in each minor version of tofu, from 0.1 to 0.5, grouped by area.
order: 7
updated: 2026-10-09
---

What changed for you in each minor version, grouped by area. Every change is
in [CHANGELOG.md](https://github.com/pehcastro/tofu/blob/develop/CHANGELOG.md),
which the binary also carries.

tofu is 0.x and stays 0.x, so a minor version can change a verb, a flag or a
file format. This page says which.

## Reading it from tofu

```
tofu changelog [--all] [--json]
```

`tofu changelog` prints what changed since the version you last read and
records the version; `--all` prints every version and records nothing.

## 0.5

The interface you use today, sub-agents that work like a team, and a library
per language.

- **Interface**: one top row and four screens, chat, sub-agents, file edits
  and shells; a settings screen with search; `tofu --continue` opens on the
  conversation it continues.
- **Sub-agents**: named sub-agents, each with its own model, rules and
  references. They run at the same time and in the background while the lead
  stays with you, and report back with what they ran.
- **Language agents**: `ts-dev`, `go-dev`, `py-dev` and `rust-dev`, each sent
  back until its language's checks ran after its last edit.
- **Library**: rules per language and per frontend framework, each tied to
  the check that catches it, and `tofu rules add` and `off` for your own.
- **Large repositories**: warm typecheck and test runners. A 33-file
  TypeScript rename went from 675 s to 94 s, and a first search from 42.8 s
  to 187 ms.
- **Browser**: tofu reads and drives your own Chrome from its own binary. The
  classifier loop went from 48 decisions per action to 7, and tabs tofu
  opened close when the run ends.
- **Setup**: keys live in the credential store, new models arrive with
  `tofu models reload`, `tofu docs` answers how-to questions, and tofu reads
  its own `.tofu` setup, not another harness's.
- **Memory and learning**: `/remember` or the lead keeps a short rule for
  every later session, in four scopes from you everywhere to the team in
  one repository; the episode log keeps a project's whole conversation as a
  tree the lead zooms into; and `tofu learn` reads your sessions for the
  correction you keep repeating and proposes a fix.
- **Asking and status**: the lead asks you with options and a recommended
  one without stopping the work, a sub-agent's ask costs the lead one quiet
  answer, and tofu reports its state to the terminal over OSC 7501.
- **Frontends**: `tofu serve` carries the boards, side chats, memory,
  approvals, questions and quota the desk app draws.
- **Sub-agents under control**: the lead can read, diagnose, stop, kill,
  release and back up any sub-agent, and a sub-agent's row says what it is
  doing.
- **Context**: a conversation keeps one name across forks, a fork carries
  the work in progress, `tofu --continue` opens in under a second, and every
  message carries an id you can quote.
- **Pictures and updates**: `read` shows the model an image file, and tofu
  updates itself, telling an open session when a newer tofu is installed.
- **Scratchpad**: each agent writes temporary files, logs and build caches in
  its own folder outside your repository, `tofu scratch` lists them and
  `clean` removes old ones. See [Scratchpad](/docs/features/scratchpad).
- **Boardy**: local project management in markdown files, with managers,
  triage, board rules, views and sprints, and a lead that can work from a
  board's tickets. See [Boardy](/docs/features/boardy).
- **Project folders**: a project's state lives in `<name>-<hash>`, so two
  paths never share one, and a moved repository can be relinked.
- **Long sessions**: the sub-agents screen opens in under 10 ms where it took
  about 250, and the shells screen reads only the new end of each log.
- **Frontends, more**: one `tofu serve` keeps several sessions running at
  once, and sends account and settings changes as they happen.

## 0.4

The name tofu, the money rule, and the first measured wins.

- **Name**: boji became tofu, the binary and the `.tofu` folder, with history
  copied across.
- **Models**: a model is named by what pays for it, `claude-sub/...` or
  `anthropic/...`; effort is a choice beside the model, `medium` by default.
- **Accounts**: several accounts per subscription, and tofu picks the one with
  room and moves when it runs out.
- **Output the model reads**: a shell result arrives cut, `grep` gave way to
  `search` at 17 KB where it returned 133 MB, and `fetch` turned a 67,692 byte
  page into 37,274.
- **Rules**: the prompt carries only the rules that fire for the task;
  `tofu run --show-prompt` and `tofu rules index` show which and why.
- **Safety**: an edit to a file the turn has not read is refused, and
  `turnMaySpawn` decides whether a turn may spawn sub-agents.
- **Interface**: the mouse, a progress line, ids you can type to jump, and
  `tofu frame` and `tofu drive` to read the screen without a terminal.

## 0.3

The app, and the gate that can refuse.

- **The app**: `tofu` with no arguments opens it in your directory, with tabs,
  a settings view, a file edits tab and a slash command menu.
- **Gate**: `--gate off|shadow|enforce` per run; under `enforce` a deny
  refuses the call and the turn takes another path.
- **Sub-agents**: a turn can hand work to a child that owns its own paths.
- **Context**: a session that outgrows its budget forks instead of being
  rewritten, and the first request caches the whole instruction prefix.
- **Daily use**: paste a screenshot with `ctrl+v`, copy the last answer with
  `ctrl+y`, and type while a turn runs to queue the next message.

## 0.2

The record, under the name boji: `why` explains any decision, `replay` scores past
decisions against new thresholds with no network, and `check` and
`label` judge and correct a real command.

## 0.1

The instrument, under the name boji: `judge` asks the classifier a typed question set and
records every answer in an append-only ledger, with `doctor` and
`version`.
