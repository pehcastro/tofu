---
id: contract
domain: general
document: internal/subagent/contract.go and internal/turn/spawn.go, read in this repository
---

# What a sub-agent hands back at the end of a ticket

A spawned sub-agent that was given a ticket answers in two parts: the ordinary reply, and a fenced json block that turns each acceptance line into evidence rather than a claim.

## The block a sub-agent sends

```json
{"claims": [{"line": "the exact acceptance line", "command": "the command run for it", "output": "what that command printed", "met": true}], "blocked": ["a line that could not be attempted, and why"]}
```

`line` has to match an acceptance line's own text once both sides are trimmed of surrounding whitespace, or it is never matched to anything and the claim is thrown away. A paraphrase credits nothing.

## An omission is a fact about the two fields, not about met

A claim counts as omitted when both `command` and `output` are empty after trimming, whatever `met` says. `met` is the sub-agent's own belief and is never treated as a verdict, so a sub-agent that writes `"met": true` with nothing behind it still reads as an omission. A command that ran and printed nothing, such as a formatter that found no complaint, is not an omission: the command field is filled and that is enough.

## The last block wins, on purpose

A message can carry more than one fenced json block, such as a sub-agent pasting the contents of a file it just edited before it reports. The parser takes the last one it finds and ignores the rest, so a sub-agent that shows other json has to put its own contract block after it. Taking the first block would silently credit whatever json happened to appear first, which is worse than taking none.

## The two states that are not an omitted line

A sub-agent given no ticket, or a ticket whose brief carries no `## Acceptance` section, sends no block at all and is marked as having no contract rather than an empty one. A sub-agent stopped mid-run by a cap is marked partial, and every line it never reached counts as neither met nor omitted, because it was never given the chance to answer.

## What it does not check

A clean block, with a real command and real output behind every claim, is evidence that something ran. It is not a verdict that the command's output actually satisfies the line: nothing here checks an exit code or the intent behind an acceptance line against what came back. That reading is done by whoever evaluates the contract, not by the shape of the contract itself.
