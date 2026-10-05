---
title: Structure
description: The library is the folder of plain files that tells tofu's agents how to work, compiled into the binary and split into domains.
order: 1
updated: 2026-10-04
---

The library is the [`library/`](https://github.com/pehcastro/tofu/tree/develop/library)
folder of the repository: rules, references and agents, plus the data tofu
runs on, such as the model catalog and the classifier questions. It is
embedded in the binary when tofu is built.

| Kind | File | What it is |
|---|---|---|
| Rule | `<domain>/rules/<id>@1.yaml` | One instruction, sent only where it fires |
| Reference | `<domain>/references/<name>.md` | A long document, placed whole for the agent that names it |
| Agent | `<domain>/agents/<name>.md` | A sub-agent: model, tools, references, gate |

The top folders are **domains**. `general` reaches every agent. `dev` reaches
agents that write code, with a folder per language and framework: `go`,
`python`, `rust`, `typescript`, `frontend`, `react`, `svelte` and `vue`. `qa`
reaches the `qa` agent. The rest of the folder is tofu's own data: `models/`,
`subscriptions/`, `questions/`, `decisions/`, `docs/`, `web/` and `tools/`.

## Why a built-in library

**A coding harness should know how to code on the first run.** Claude Code
and Codex start empty and you add the skills. tofu brings the orchestrator
and sub-agent rules, the language agents and their references.

**Embedded, so there is nothing to install** and every machine runs the same
library as the binary it came with.

**Domains keep each prompt to its own rules.** Sending a rule only to the
agents of its domain took the lead from 24 rules to 20 and the qa agent from
14 to 5, on the same task.

**Your files sit on top**, never inside: `~/.tofu` for every project and
`.tofu` for one. See [Customization](./customization).

## Seeing and changing it

- **See what's loaded**: `tofu library`, and `tofu rules list` for every rule
  that runs.
- **Try a change to the library**: run tofu from a folder that holds a
  `library/` folder; its rules replace the built-in ones.
- **Add your own**: see [Customization](./customization) and
  [Contributing](./contributing).

## Commands

```sh
tofu library
```

```text
Library · 4 layers                                             ✓ nothing refused

  models         29
  subscriptions  2
  roles          2
  questions      14
  docs           12 pages · 67 index entries
  proxy          off · library

layers
  library   library
  catalog   ~/.tofu/catalog
  global    ~/.tofu
  project   F:\localhost\ephem-sh\tofu\.tofu

domains · F:\localhost\ephem-sh\tofu\library
  ✓ dev      126 rules · 4 agents · 31 references
  ✓ fetch    1 threshold
  ✓ general  22 rules · 7 thresholds · 2 agents · 5 references
  ✓ qa       9 rules · 2 skills · 1 agent · 4 references
  ✓ shell    1 threshold
```
