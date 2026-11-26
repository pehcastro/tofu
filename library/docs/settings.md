---
topic: settings
title: Settings
summary: every setting, its default, what it takes, and how to change it
verbs: settings
---

## What it is

A setting is a named switch, like `theme` or `gatePrompt`. Each one has a default, and you only write the ones you want different.

The settings come in groups:

- Appearance: theme, density, animations, statusBar, colorMode
- Models & roles: continueOnSubscription, subscriptionOrder, continueOnAPIModel
- Interaction: composer, gatePrompt, gateAskWaitSeconds, gateAskTimeout, askPersonWaitSeconds, readBeforeEdit, autoUpdate
- Context: chatShowsTools, agentFeeds, showThinking, thinkingSummary, images, projectInstructionsCap, contextCeiling, fork, autoCompaction, instructionSources, skills, memory, autoMemory, episodes, memoryFromAllUsers, memoryModel, learn
- Files: diffContext, hyperlinks, groupByAgent
- Shell: persistentRegistry, logTail, killConfirm, shell
- Turn: decisionCap, turnMaySpawn, oneWorkingSessionPerProject, planEveryStep, planAdvisor, sideChatAccess, subAgentsPerTurn, subAgentDepth, leadCache, subAgentCache, subAgentCheckSeconds, subAgentWatchSeconds, verifySubAgents, doneReview, agentSources, modelTier.genius, modelTier.smart, modelTier.worker, modelTier.dumb
- Projects: projectManagement (boardy, the ticket board), boardFlow (the words boardy uses), scratchCleanupDays, scratchMaxGB and scratchRoots (the scratchpad, scratchRoots global only)
- Browser: browser, browserDriver, browserSteps, browserModel, browserEffort, browserCursor, browserProfile, browserHeadless, browserSleepMinutes

`gatePrompt` is `auto` by default, which is auto mode: Jev decides at every gate and the work does not stop for you. Only a call it denies is refused, and the model is told why. A call it would ask about runs, and shows in the chat with its verdict. Every decision is a ledger row `tofu why` reads. `tofu settings set gatePrompt ask` turns asking on: a call Jev would ask about waits for your answer, `1` to allow it once, `2` to deny it, `3` to allow it here from now on. A project hook that asks, a rule override and the model changing a setting ask you in both modes: no classifier answers the first two, and the last could otherwise turn its own gate off. `tofu run` has nobody to ask, so there an ask is refused in both modes.

`leadCache` is how long the lead's prompt cache lives on a subscription, `1h` by default. `5m` writes the whole cache at the cheaper five minute rate, and a pause past five minutes then writes it again. `history-5m` keeps the tools and system prompt an hour and writes the conversation at five minutes, so a pause rewrites only the conversation. `subAgentCache` is the same choice for a sub-agent, `1h` or `5m`. `tofu session trace` shows each request's write at the lifetime it got.

`continueOnSubscription`, `subscriptionOrder` and `continueOnAPIModel` say where work goes when every account of a subscription is at its limit: a model on another subscription, in that order, then one paid by your key. With none set the work stops and names them. `tofu docs usage` has more.

A setting changed in one window, by `tofu settings set` or by hand, reaches every open window and the project's host on the next turn, with no reload.

## The detail

Each setting's own line is two sentences at most. The rest is here.

- `continueOnSubscription`: when every account on the subscription a turn or a sub-agent runs on is at its limit, it continues on the model picked here for another subscription provider, one model per provider, the first in `subscriptionOrder` with an account left. Shown, with `subscriptionOrder`, when you have accounts on more than one subscription provider. Empty `subscriptionOrder` tries providers in the order they were picked.
- `continueOnAPIModel`: used by a turn or a sub-agent once no subscription has an account left.
- `gatePrompt`: under `auto`, only what Jev denies is refused, and what it would ask about runs and shows its verdict in the chat. Under `ask`, a call waits until you answer or `gateAskWaitSeconds` passes.
- `gateAskWaitSeconds`: applies under `auto` and `ask` alike. A cron job that fires while a call waits decides it at once. The next ask reads the new value.
- `gateAskTimeout`: a call only you may allow changes settings, hooks or the harness, which is why `refuse` is the default.
- `askPersonWaitSeconds`: the question stays open and a later answer still reaches the lead. Under `ask` the lead waits with no timeout. The next turn reads the new value.
- `autoUpdate`: the lookup runs at most once an hour. With it off, `tofu update` installs the release. A build from source never updates.
- `thinkingSummary`: `omitted` means no thinking reaches the sub-agents feed.
- `contextCeiling`: a model whose window is smaller stops at its window less the answer's reserve. `TOFU_CONTEXT_CEILING` and `--context-ceiling` win over it, and the next turn reads the new value.
- `fork`: off keeps one conversation however long it grows, with `autoCompaction` the only thing holding it near the ceiling. A request over the model's window is still refused before it is sent. Sub-agents follow the lead, and the next turn reads the new value.
- `autoCompaction`: off sends every tool result in full until the conversation reaches the ceiling, where it forks unless `fork` is off too. Sub-agents follow the lead, and the next turn reads the new value.
- `instructionSources`: `agents-first` sends AGENTS.md and `claude-first` sends CLAUDE.md. They are read from the working directory up to its git root, and the home directory gives only `~/.tofu/AGENTS.md`.
- `skills`: the skill folders are `.tofu`, `.agents` and `.claude` in the project, and only `~/.tofu/skills` in the home directory.
- `memory`: on also lets the lead offer one short rule. `tofu memory` lists the entries.
- `autoMemory`: answering always on a memory card turns it on.
- `episodes`: on also sends their view after the memory and through every fork, and offers the `history` tool's `zoom` and `recall` modes. Off keeps none and offers neither mode, while the memory still goes.
- `memoryFromAllUsers`: the memory lives in the repository's `.tofu` folder. Project local memory applies to everyone either way.
- `memoryModel`: each summary is one line of at most 512 bytes. Empty takes the model the library marks `memory: yes` for the session's own source, never the OpenRouter key.
- `learn`: it prints how many windows and bytes it sends first, and writes each proposal with your lead model.
- `persistentRegistry`: off stops every shell on exit, even when tofu is killed. Read on the next start.
- `shell`: one of the offered values or a path to a shell. Empty resolves the person's own shell.
- `planEveryStep`: each sub-agent keeps a plan of its own and sees that one. The next turn reads the new value.
- `planAdvisor`: `shadow`, the default, has the advisor sub-agent read each plan the lead sets against your words and write what is missing or extra to the trace, unseen by the lead; `off` runs none. See `tofu docs agents`.
- `sideChatAccess`: `read` writes nothing, `notes` writes .md and .html files, `files` writes anywhere, and any other value is a comma list of globs. A side chat never spawns, schedules, remembers or changes a setting, whatever this says.
- `subAgentsPerTurn`, `subAgentDepth`, `subAgentCache` and `subAgentCheckSeconds`: the next spawn reads the new value.
- `subAgentCheckSeconds`: the check is built from what tofu records: the sub-agent's steps, the files it changed, its gate and its last tool.
- `subAgentWatchSeconds`: the chat line says what the call is doing, such as waiting for the orchestrator's answer, running bash or building. A sub-agent with no new output, step or request for this long is stalled.
- `verifySubAgents`: the lead reads what the sub-agent changed, runs the build or the tests again and opens what it made. Off takes the report as the result unless you ask for a check. `tofu rules off verify_sub_agents` turns it off too, whatever this says.
- `doneReview`: `screen` sends a sub-agent back while an acceptance line on its ticket has no evidence entry naming it, up to the round cap, and the lead's report names each line. `typed` asks Jev after the screen and falls back to `screen`, saying so in the chat, when no classifier is bound. `cheap` sends back a sub-agent that ran no tool without failing; `off` reviews nothing. A sub-agent with no ticket passes the screen. `--done-review` and `TOFU_SERVE_DONE_REVIEW` win over it. The next turn reads the new value.
- `agentSources`: a comma list, inside the project. The home directory gives only `~/.tofu/agents`, and the library is always read.
- `projectManagement`: `boardy`, the default: a spawn names a ticket, its owns become the sub-agent's paths and the sub-agent moves it to review. A project with no board gets one from the lead's `board_create`, or from tofu at the first spawn of a sub-agent that edits; `off` keeps no board.
- `boardFlow`: jira says ticket, board, epic, sprint and points. linear says issue, team, project, cycle and estimate.
- `scratchCleanupDays`: only a folder with no tofu running it is deleted, its processes stopped first.
- `sessionStaleDays`: no activity means no new event, request or open. The deletion runs when the project's host starts or on `tofu session clean`. A session a kept one continues or branches from, the head and one open now are kept.
- `browserDriver`: `subagent` hands the steps to a browser sub-agent on `browserModel`, spawned with a task and a tab. `steps` gives the model `browser_observe` and `browser_act`. `goal` gives it `browser_do`, where Jev picks each step toward a whole goal.
- `browserModel`: a subscription model. Empty takes the dumb tier, then the worker tier, then the turn's own model.
- `browserEffort`: used when the model lists effort. The model's own default is the lowest it lists at or above low.
- `browserCursor`: it never changes what tofu reads or clicks, and is read when Chrome starts the relay.
- `browserProfile`: tofu starts the installed Chrome on that profile's own folder under the tofu home, so a site signed in once with `tofu browser profile login` stays signed in.
- `browserHeadless`: read when that Chrome starts, and never by your own Chrome.
- `browserSleepMinutes`: read each time the last tofu leaves the relay, so a change needs no restart. Asleep, the relay holds no tab list and the extension posts no tab events; the first call that needs Chrome wakes it.

## Where it lives

- `~/.tofu/settings.json`: yours, in every project
- `.tofu/settings.json` in a project: that project only

The project file wins over yours, and yours wins over the default.

## Change it

    tofu settings set <key> <value>

writes your file. Add `--scope project` to write the project's instead:

    tofu settings set --scope project gatePrompt ask

A true or false setting takes `true` or `false`. A list takes commas and no spaces, like `tofu,claude`. A value the setting does not take is refused and nothing is written. An older value a setting used to take, like `AGENTS.md,CLAUDE.md` for `instructionSources`, is read and written as the choice it became. In the app, type `/settings` for the same list with a search.

A setting marked (on the next start) is read once when tofu opens, so quit and open it again.

## Check it

    tofu settings

prints every setting by group, its value, and `project`, `global` or `default` for where it came from, with `●` on the ones you set.

    tofu settings get <key>

prints one value. Both take `--json` and print one JSON document. `set` prints one `~` line with the file and `→ undo:` with the command that puts the old value back.

## Undo it

Set it back to the default shown below, or delete its line from the settings file. With the line gone, the next file down wins.

## Every setting
