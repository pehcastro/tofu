---
topic: settings
title: Settings
summary: every setting, its default, what it takes, and how to change it
verbs: settings
---

## What it is

A setting is a named switch, like `theme` or `gatePrompt`. Each one has a
default, and you only write the ones you want different.

The settings come in groups:

- Appearance: theme, density, animations, statusBar, colorMode
- Interaction: composer, gatePrompt, readBeforeEdit, autoUpdate
- Context: chatShowsTools, agentFeeds, showThinking, thinkingSummary, images, projectInstructionsCap, instructionSources, skills, memory, autoMemory, memoryFromAllUsers, learn
- Files: diffContext, hyperlinks, groupByAgent
- Shell: persistentRegistry, logTail, killConfirm, shell, foldHidesShell
- Turn: decisionCap, turnMaySpawn, oneTurnPerProject, sideChatAccess, subAgentsPerTurn, subAgentDepth, subAgentCache, subAgentCheckSeconds, subAgentWatchSeconds, scratchCleanupDays, scratchMaxGB, projectManagement, boardFlow, verifySubAgents, agentSources, modelTier.genius, modelTier.smart, modelTier.worker, modelTier.dumb
- Browser: browser, browserDriver, browserSteps, browserModel, browserEffort, browserCursor

`gatePrompt` is `auto` by default, which is auto mode: Jev decides at every
gate and the work does not stop for you. Only a call it denies is refused,
and the model is told why. A call it would ask about runs, and shows in the
chat with its verdict. Every decision is a ledger row `tofu why` reads.
`tofu settings set gatePrompt ask` turns asking on: a call Jev would ask
about waits for your answer, `1` to allow it once, `2` to deny it, `3` to
allow it here from now on. A project hook that asks, a rule override and
the model changing a setting ask you in both modes: no classifier answers
the first two, and the last could otherwise turn its own gate off.
`tofu run` has nobody to ask, so there an ask is refused in both modes.

`subAgentCache` is how long a sub-agent's prompt cache lives, `1h` by
default. `tofu settings set subAgentCache 5m` writes a sub-agent's cache at
the cheaper five minute rate; a sub-agent idle past five minutes then
writes its prefix again. The lead keeps one hour either way, and
`tofu session trace` shows each request's write at the lifetime it got.

## Where it lives

- `~/.tofu/settings.json`: yours, in every project
- `.tofu/settings.json` in a project: that project only

The project file wins over yours, and yours wins over the default.

## Change it

    tofu settings set <key> <value>

writes your file. Add `--scope project` to write the project's instead:

    tofu settings set --scope project gatePrompt ask

A true or false setting takes `true` or `false`. A list takes commas and no
spaces, like `tofu,claude`. A value the setting does not take is refused and
nothing is written. An older value a setting used to take, like
`AGENTS.md,CLAUDE.md` for `instructionSources`, is read and written as the
choice it became. In the app, type `/settings` for the same list
with a search.

A setting marked (on the next start) is read once when tofu opens, so quit
and open it again.

## Check it

    tofu settings

prints every setting by group, its value, and `project`, `global` or
`default` for where it came from, with `●` on the ones you set.

    tofu settings get <key>

prints one value. Both take `--json` and print one JSON document. `set`
prints one `~` line with the file and `→ undo:` with the command that puts
the old value back.

## Undo it

Set it back to the default shown below, or delete its line from the
settings file. With the line gone, the next file down wins.

## Every setting
