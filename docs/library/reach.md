---
title: Reach
description: tofu library reach counts, across every session recorded in a project, which rules, references, skills, agents and tools reached an agent, and lists the ones that never did.
order: 6
updated: 2026-11-01
---

`tofu library reach` reads every session recorded for the project you run it
in and counts each library item against them: the rules, references, skills
and agents tofu ships, and every tool a session offered. `--never` lists the
items that never reached an agent instead.

| Kind | Reached means | Held means | Calls means |
|---|---|---|---|
| Rule | It fired into a prompt, or a check found it in a tool result | It was weighed for a prompt and held back | |
| Reference | It was offered in a sub-agent's prompt, or loaded | | Loads with the `reference` tool |
| Skill | It was listed or loaded into a prompt | It was hidden from the listing | Loads with the `skill` tool |
| Agent | Its definition was a sub-agent's prompt | | |
| Tool | A request offered it | | Calls the model made |

Each count is of sessions, so a rule that fired in twenty turns of one session
counts once. A tool counts as used only when it is called, so a tool offered
in every session and never called is in `--never`.

## Why it exists

**A library item that reaches no agent costs lines and buys nothing.** A rule
held back in every task, a reference no sub-agent is given, and a tool that is
offered on every request and never called all look the same as a used one
until something counts them. `tofu session trace --kind library` shows the
items of one session, turn by turn; reach adds them up across all of them.

## How to

    tofu library reach
    tofu library reach --never
    tofu library reach --json

The first lists the items that reached an agent, grouped by kind, each with
the sessions it reached, the sessions it was held in, its calls and when it
was last seen:

    Library reach · 2 sessions                                57 reached

    1 skill
      ✓ debug  reached 1 session · 1 call · last 6s ago

    3 tools
      ✓ read   offered in 2 sessions · 2 calls · last 5s ago

A project with no session prints that no session is recorded and exits 0.

## API

`--json` prints `{tofu, verb, ok, at, data, problems}`. `data` holds
`sessions`, the number read, `never`, whether `--never` was given, and
`rows`, one per item:

| Field | Meaning |
|---|---|
| `kind` | `rule`, `reference`, `skill`, `agent` or `tool` |
| `name` | The rule id, or the file or tool name |
| `reached` | Sessions it reached; for a tool, sessions that offered it |
| `held` | Sessions it was held back in |
| `calls` | Tool calls, or loads of a skill or reference |
| `last_at` | When it was last seen, left out when never |
