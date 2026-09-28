---
topic: files
title: Files tofu reads and writes
summary: every path tofu uses in your home and in a project, and which one wins
verbs: settings, agents, login, migrate, session
---

## What it is

tofu keeps what it knows about you in `~/.tofu` and what it knows about one
project in that project. Nothing else in your home is read: not `~/.claude`,
not `~/.agents`. The home gives tofu only `~/.tofu`.

## Where it lives

In your home, `~/.tofu`:

- `settings.json`: your settings, written by `tofu settings set`
- `agent.db`: the credentials `tofu login` stores, subscriptions, the
  openrouter key and the brave search key alike. tofu keeps no `.env` here, and moves an old one
  into `agent.db` on the next start
- `AGENTS.md`: instructions sent with every task, in every project
- `agents/`: your own sub-agents, one file each, written by `tofu agents add --global`
- `rules/`: your own rules for every project, written by `tofu rules add --global`
- `skills/`: your own skills, one folder each
- `agent-models.yaml`: the model a sub-agent runs, chosen in the app or with `tofu agents set --global`
- `models/`, `subscriptions/`, `roles/`: your changes to the model list
- `keybindings.json`: your shortcuts in the app
- `changelog-seen`: the last version `tofu changelog` showed you
- `quota/`: the quota readings `tofu usage --history` prints
- `projects/<project>/`: sessions, logs and running shells, one folder per project

In a project:

- `.tofu/settings.json`: settings for this project only
- `.tofu/agent-models.yaml`: sub-agent models for this project only, written by `tofu agents set`
- `.tofu/rules/`: rules for this project only, written by `tofu rules add`
- `.tofu/agents/`, `.agents/agents/`, `.claude/agents/`: sub-agents, in the order the `agentSources` setting names. `tofu agents add` writes into `.tofu/agents/`
- `.tofu/skills/`, `.agents/skills/`, `.claude/skills/`: skills, from the working directory up to the git root
- `AGENTS.md` or `CLAUDE.md`: instructions, from the working directory up to the git root, the first of `instructionSources` in each folder
- `.tofu/models/`, `.tofu/subscriptions/`, `.tofu/roles/`: model changes for this project only
- `library/`: when the working directory has a folder of this name, its rules replace the ones tofu ships

Which one wins: the project wins over your home, and your home wins over
what tofu ships. A setting, a sub-agent or a model set in the project is the
one used there.

## Change it

Edit a file by hand, or use the command that writes it:

    tofu settings set <key> <value>
    tofu settings set --scope project <key> <value>
    tofu login claude-sub

Older versions kept sessions and logs in the project's `.tofu`. Move them
into `~/.tofu`:

    tofu migrate --dry-run
    tofu migrate

## Check it

    tofu settings

prints every setting and the file it came from.

    tofu agents

lists every sub-agent, where it was read from, and the model it runs.

    tofu session list

lists the sessions recorded for this project.

## Undo it

Delete the line or the file you added. With it gone, the next layer down
wins: your home, then what tofu ships. Leave `agent.db` alone; use
`tofu login --disable <number>` to set a credential aside.
