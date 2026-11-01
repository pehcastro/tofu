---
topic: library
title: What reached your agents
summary: tofu library reach counts which rules, references, skills, agents and tools reached an agent across this project's sessions, and lists the rest
verbs: library
---

## What it is

`tofu library reach` reads every session recorded for this project and
counts each library item against them: every rule, reference, skill and
agent tofu ships, and every tool a session offered. `--never` lists the
items that never reached an agent instead.

## Where it lives

- reached: for a rule, it fired into a prompt or a check found it in a
  tool result; for a reference or a skill, it was offered or loaded; for
  an agent, its definition was a sub-agent's prompt; for a tool, a request
  offered it
- held: a rule weighed for a prompt and held back, or a skill hidden from
  the listing
- calls: tool calls, and loads of a skill or reference with the `skill`
  and `reference` tools
- each count is of sessions, so twenty turns of one session count once
- a tool counts as used only when it is called, so a tool offered in every
  session and never called is listed by `--never`

## Change it

Nothing to set. `tofu session trace <name> --kind library` shows the same
items for one session, turn by turn.

## Check it

    tofu library reach
    tofu library reach --never
    tofu library reach --json

On a project with two sessions the first prints:

    Library reach · 2 sessions                                34 reached

    32 rules
      ✓ announce_spawn    reached 2 sessions · held in 1 session · last 1m ago
      ✓ answer_asks       reached 2 sessions · held in 1 session · last 1m ago

and `--never` prints the rest, each held or never offered:

    Library never reached · 2 sessions                 249 never reached

    147 rules
      ○ bounded_change    held in 2 sessions · last 1m ago
      ○ browse            held in 2 sessions · last 1m ago

A `✓` row reached an agent at least once and a `○` row never did.

The first groups the items by kind, each with the sessions it reached,
the sessions it was held in, its calls and when it was last seen. A
project with no session prints that no session is recorded and exits 0.
`--json` rows carry `kind`, `name`, `reached`, `held`, `calls` and
`last_at`.

## Undo it

It changes nothing; it only reads the sessions under `~/.tofu`.
