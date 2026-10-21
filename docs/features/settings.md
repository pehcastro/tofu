---
title: Settings and reload
description: Two settings files, the project over yours, read again at every task, searchable in the app.
order: 8
updated: 2026-10-12
---

A setting is a named switch with a default, like `gatePrompt` or
`subAgentsPerTurn`. You write only the ones you change, in
`~/.tofu/settings.json` for every project or `.tofu/settings.json` for one.
The project wins over yours, and yours over the default. 60 settings sit in
eight groups: Appearance, Interaction, Context, Files, Shell, Turn, Projects
and Browser. Projects holds boardy, the ticket board, and the scratchpad.

![Ctrl+K search over the settings, with browser typed](./media/tui-search.png)

## Why there is no restart

**No restart.** tofu reads settings, rules, skills, sub-agents and
instruction files again when each task starts, so a change counts from your
next message. Only `persistentRegistry` waits for a restart.

**You can see where a value came from.** Every value says `default`,
`global` or `project`, so a project that behaves differently explains itself.

**A bad value never lands.** A value a setting doesn't take is refused, and
nothing is written.

## Auto mode is the default

**Work does not stop to ask.** With `gatePrompt` on `auto`, Jev decides at
every gate, and only a call it denies is refused. A call it would ask about
runs and shows in the chat with its verdict, and every decision is a row
`tofu why` reads, so nothing happens out of sight. The model changing a
setting always waits for you, so it can never turn its own gate off. When
Jev can't answer, the call is refused with the reason rather than run on a
guess. A question the lead asks you with options doesn't stop the work
either: its recommended option stands after `askPersonWaitSeconds`. See
[Asking you](/docs/features/asking).

**Asking is one setting away.** `tofu settings set gatePrompt ask` makes a
call Jev would ask about wait for you: allow once, deny, always here, never
here, or cancel. Always here and never here stand for that session, also
after a restart or `tofu --continue`.

## Changing a setting

- **Browse and change**: `/settings`, or `Ctrl+K` to search.
- **Set from the shell**: `tofu settings set gatePrompt ask`, or
  `--scope project` for this project only.
- **Undo**: `set` prints the command that puts the old value back, or delete
  the line from the file.
- **Check what tofu read** after editing a file by hand: `/reload` or
  `tofu reload` prints what changed since the last reload.

## Commands

```sh
tofu settings
```

```text
Turn
    decisionCap             0                                default
    turnMaySpawn            true                             default
    oneWorkingSessionPerProject true                         default
    planEveryStep           true                             default
    sideChatAccess          read                             default
    subAgentsPerTurn        10                               default
    subAgentDepth           2                                default
    subAgentCache           1h                               default
    subAgentCheckSeconds    1800                             default
    subAgentWatchSeconds    600                              default
    verifySubAgents         true                             default
    agentSources            tofu,agents,claude               default
Projects
    projectManagement       off                              default, off until the project has a board: tofu boardy init
    boardFlow               jira                             default
    scratchCleanupDays      7                                default
    scratchMaxGB            50                               default
```

`oneWorkingSessionPerProject` keeps one working session per project:
while one session in the project is working, another session that
starts work is refused until it ends. Turn it off to let several
sessions in one repository work at once:
`tofu settings set oneWorkingSessionPerProject false`. A settings file
that still holds `oneTurnPerProject` reads as the new key and is
rewritten under it on the next write.

`thinkingSummary` decides what a model sends back of its thinking.
`summarized`, the default, asks for a summary, which streams to the
sub-agents feed. `omitted` asks for none and sends the thinking request the
vendor's own command line sends: Claude Code's on `claude-sub` and codex's on
`codex-sub`. Either way, the thinking tokens Sonnet spends are recorded in
the session.

`tofu settings get <key>` prints one value. Both take `--json`.
