---
id: contract
domain: general
document: internal/subagent/contract.go, internal/subagent/finish.go and internal/turn/finish.go, read in this repository
---

# What a sub-agent hands back at the end of its work

A spawned sub-agent ends by calling finish. The call is its report: the lead reads it in place of the sub-agent's last message, and the desk draws it. A sub-agent that ends its turn without calling finish is sent back once to call it; one that ends without it again is reported blocked, with its last message as the report and a finding saying nothing checked it. A sub-agent stopped by a cap is not sent back.

## The call

```json
{"verdict": "done", "summary": "the outcome in one to three sentences",
 "files": [{"path": "src/users.ts", "change": "adds the users route"}],
 "checks": [{"command": "go test ./internal/x/", "exit": 0, "excerpt": "ok"}],
 "claims": [{"line": "the exact acceptance line", "command": "the command run for it", "output": "what that command printed", "met": true}],
 "findings": [{"bucket": "consider", "text": "what to look at", "cite": "path:line"}],
 "confidence": {"level": "high", "reason": "why"},
 "openQuestions": [{"question": "what is unknown", "settleBy": "what would settle it"}],
 "asks": [{"kind": "deferred", "ask": "what the lead should decide"}],
 "consulted": ["a file or command"], "body": "markdown for what the fields cannot hold"}
```

verdict is one of done, done_with_concerns, blocked and needs_context. A field the call names but leaves empty, or a field it does not know, refuses the call with the field named, and the turn goes on until a call is accepted.

## What tofu checks

tofu compares files with the writes and edits it saw the sub-agent make, and each check's exit code with the command it saw run. A claimed file it never saw written is flagged act_on. A file it saw written that the report does not name is added and flagged consider. A check whose exit code differs takes the exit code tofu saw, and is flagged act_on. A check tofu never saw run is flagged consider. Any concern turns done into done_with_concerns, and a run that ended blocked or waiting on a question keeps that verdict whatever the call says.

## Claims, when the brief names a ticket

A sub-agent given a ticket with an ## Acceptance section puts one claim per acceptance line in claims. `line` has to match an acceptance line's own text once both sides are trimmed of surrounding whitespace, or it is never matched to anything and the claim is thrown away. A paraphrase credits nothing.

A claim counts as omitted when both `command` and `output` are empty after trimming, whatever `met` says. `met` is the sub-agent's own belief and is never treated as a verdict. A command that ran and printed nothing, such as a formatter that found no complaint, is not an omission: the command field is filled and that is enough.

A sub-agent given no ticket, or a ticket whose brief carries no `## Acceptance` section, has no contract rather than an empty one. A sub-agent stopped mid-run by a cap is marked partial, and every line it never reached counts as neither met nor omitted.

With `projectManagement` set to `boardy`, a spawn names its ticket by id, such as `DEMO-2`, and the contract reads that ticket from the board rather than searching the brief. The acceptance lines are the revision in force, the last `## Acceptance` section of the ticket file. With the setting `off` the contract reads the brief: the first ticket-shaped id in it and its `## Acceptance` section.

A sub-agent that ends without finish can still carry its claims as the last fenced json block of its last message, in the shape `{"claims": [...], "blocked": [...]}`, and the contract reads them from there.

## What it does not check

A clean claim, with a real command and real output behind it, is evidence that something ran. It is not a verdict that the output satisfies the line. That reading is done by whoever evaluates the contract.
