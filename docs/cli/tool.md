---
title: One tool call by hand
description: tofu tool lists the tools a run offers and runs one call through the loop's own call path, printing the exact text the model would receive.
order: 4
updated: 2026-11-06
---

`tofu tool <name> '<json>'` runs one tool call outside a turn and prints the result text the model
would receive, byte for byte. `tofu tool list` prints the tools a run offers. No model is asked, so the
same command on the same files prints the same output.

## Why it exists

**A tool property is a command anyone reruns.** Inside a turn a tool runs only when a model decides to
call it, and its result is mixed into a conversation. `tofu tool` runs the call the loop would run,
through the same tool set `tofu run` and the app build, and prints what the model would have read. A
check of a tool is then one command and its output, and a second run on the same build gives the same
answer.

**What it prints is what the model reads.** The call goes through the loop's own call path: the
command proxy, PreToolUse and PostToolUse hooks, the shell sift that cuts a long bash result, the
artifact notice for a result over the result cap, and the sentence that stands in for an empty result.

## How to use it

List what the lead is offered, in the order the request sends it:

```
tofu tool list --dir .
```

List what a spawn of one sub-agent is offered:

```
tofu tool list --dir . --agent rust-dev
```

Run one call:

```
tofu tool read '{"path":"src/main.rs"}' --dir .
tofu tool bash '{"command":"go test ./..."}' --dir .
```

Run it as a sub-agent would, inside the spawn's ownership boundary. A spawn made here holds no owns, so
a write is refused with the words a sub-agent gets:

```
tofu tool write '{"path":"notes.txt","content":"x"}' --dir . --agent rust-dev
```

Bind the call to a recorded session, so `quote`, `lookup` and `recall` answer from it:

```
tofu tool quote '{"id":"[quote#3f2a]"}' --dir . --session head
```

## Reference

| Form | What it does |
|---|---|
| `tofu tool list [--dir D] [--agent NAME]` | prints the offered tool names, one per line |
| `tofu tool NAME JSON [--dir D] [--agent NAME] [--session S] [--sift free\|judged]` | runs one call and prints the result text; `--sift` picks the bash result cut as `tofu run --sift` does |

Standard error ends with a line naming each file the call created, changed or removed under the tofu
state folder in your home, or saying nothing was written.

| Exit | Meaning |
|---|---|
| 0 | the tool ran and returned a result |
| 1 | the tool failed; standard output holds the error text the model gets, starting `error: ` |
| 2 | usage: a name the run does not offer (the offered names follow), malformed JSON (the byte where it broke), or `spawn`, `message`, `subagents` and `ask`, which answer only inside a live turn |
