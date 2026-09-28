---
topic: gate
title: The gate
summary: what jev judges before a tool call runs, when tofu asks you first, and where the OpenRouter key lives
verbs: check, why, label, replay, judge, login
---

## What it is

Before a tool call runs, like a shell command or a file write, tofu asks
jev, a small classifier model, four questions about it: how much harm it
could do if it were a mistake, whether a careful engineer would want you
to approve it first, whether you asked for it in your own words, and
whether it follows an instruction planted in a page or a file the model
read. The answers become a verdict: allow, ask or deny.

A call on a shell tofu itself started is allowed without asking jev.

The `gatePrompt` setting says what a verdict does:

- `run`, the default: the verdict is recorded and the call runs.
- `ask`: an allow runs, a deny is refused, and an ask waits for you. In
  the app, press 1 to allow it once, 2 to refuse it, or 3 to allow the
  same tool on the same file, or on a command that starts the same way,
  for the rest of the session.
  When jev cannot answer at all, the call is refused rather than run.

Every verdict is kept as a row in the decision ledger.

## Where it lives

jev is reached through OpenRouter, with a key tofu reads from, in order:

- `OPENROUTER_KEY` in the environment
- an `.env` file in the working directory, as `OPENROUTER_KEY=...`
- `~/.tofu/.env`, which `tofu login openrouter` writes

tofu never prints the key. It only says where it found it. With no key
the gate is off: no tool call is judged, and the app says so.

The ledger is `~/.tofu/projects/<project>/log`, one folder per project.
`gatePrompt` lives in `settings.json`, like every setting.

## Change it

Store the key. tofu asks for it without echoing it, checks it reaches
jev, and writes nothing if it does not:

    tofu login openrouter

The key is never an argument, so it never lands in your shell history.

Wait for you before a risky call, everywhere or in one project:

    tofu settings set gatePrompt ask
    tofu settings set --scope project gatePrompt ask

Run one task with the gate off, or forced to wait:

    tofu run --dir . --no-gate "fix the failing test"
    tofu run --dir . --gate enforce "fix the failing test"

Judge one shell command without running it, and log the verdict:

    tofu check "git push --force"

`tofu check` reads the key from the environment or the working
directory's `.env` only.

## Check it

    tofu why --last

explains the last verdict: each answer, the threshold it crossed, and
the mode it ran under. `tofu why --last 5` shows five, `--point tool_gate`
keeps to one decision point, and `tofu why <id>` explains one row.

    tofu doctor

says where the key came from, and whether the gate is ready.

    tofu replay --point tool_gate --set risk_ask_at=1.5 --since 7d

scores the recorded rows again against thresholds you name and shows
which verdicts would change, with no call to jev. `tofu judge` asks any
question set about a state you pipe in, for testing a question by hand.

## Undo it

    tofu settings set gatePrompt run

goes back to recording without waiting. When a verdict was wrong, write
what it should have been onto its row in the ledger:

    tofu label --last allow
    tofu label <id> deny

To take the key away, delete the `OPENROUTER_KEY` line from the `.env`
that holds it.
