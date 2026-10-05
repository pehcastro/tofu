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
- Interaction: composer, gatePrompt, readBeforeEdit
- Context: chatShowsTools, agentFeeds, showThinking, thinkingSummary, images, projectInstructionsCap, instructionSources, skills
- Files: diffContext, hyperlinks, groupByAgent
- Shell: persistentRegistry, logTail, killConfirm, shell, foldHidesShell
- Turn: decisionCap, turnMaySpawn, subAgentsPerTurn, subAgentDepth, subAgentCheckSeconds, agentSources, modelTier.genius, modelTier.smart, modelTier.worker, modelTier.dumb
- Browser: browser, browserDriver, browserSteps, browserModel, browserEffort, browserCursor

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
