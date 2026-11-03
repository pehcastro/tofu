---
id: contract
domain: general
document: internal/project/boardy/events.go, internal/project/boardy/hook.go and internal/turn/spawn.go, read in this repository
---

# What a sub-agent hands back at the end of its work

A spawned sub-agent's last message is its report to the lead: where it ended, in plain sentences, with no heading and no bold label. On a ticket, the report on the board is built from the sub-agent's own entries on that ticket, not from its last message.

## On a ticket: three entries

1. `ticket_log` kind evidence, once for each acceptance line it proves, with `line` set to that line counted from 1 in the revision in force, and the text holding the command exactly as it ran and the output lines that matter.
2. `ticket_log` kind say, one or two sentences in the first person: where it ended and what the lead needs to know.
3. `ticket_move` to review, with `leave` holding the lines that are not its to prove, such as a line that needs the installed binary the lead drives.

```json
{"id": "DEMO-1", "kind": "evidence", "line": 1, "text": "go test ./internal/parse\nok  demo/internal/parse 0.21s"}
{"id": "DEMO-1", "kind": "say", "text": "The reader decodes UTF-8 and lines 1 and 2 are proved. Lines 3 to 5 need the installed binary, so I leave them to you."}
{"id": "DEMO-1", "to": "review", "leave": [3, 4, 5]}
```

## Each line is met, unmet or not mine to prove

A line is met when an evidence entry names it, not mine to prove when the last move to review put it in `leave`, and unmet otherwise. An evidence entry with no `line`, such as one logged before this field existed, counts for the first line nothing else covers. A command that ran and printed nothing, such as a formatter that found no complaint, is still evidence.

The `evidence_at_review` hook refuses a move to review while any line is unmet, and names each one.

## The reported entry

When the sub-agent ends, tofu writes one reported entry on the ticket. Its text is the sub-agent's last say entry, cut to two sentences, or the first line of its last message when it said nothing. It carries three fields: `verdict`, one of done, done with concerns, blocked, needs you and not possible; `acceptance`, each line with its state; and `files`, what tofu saw it write. A run stopped by a cap or an error is blocked, a run waiting on an answer needs you, and an unmet line or a concern turns done into done with concerns. The lead reads the same verdict and lines under the sub-agent's report.

A sub-agent given no ticket, or a ticket with no acceptance lines, has no acceptance and reports through its last message alone.

## When it cannot be done as asked

A sub-agent that finds its ticket cannot be done as asked, because the platform, a library or the code does not give it, says so with `ticket_not_possible` instead of the move to review, and then ends:

```json
{"id": "DEMO-1", "reason": "gpui has no backdrop filter, per its docs", "options": ["drop the blur", "use a flat tint"], "recommend": 2, "why": "a tint is what gpui can draw"}
```

`reason` says what cannot be done and how it knows: a vendor doc, an upstream issue or a probe it ran. A call with no reason is refused, naming the reason. The ticket gets a question for the person and moves to needs-decision, the report's completion is `not_possible` and its verdict `not possible`, tofu puts the question to the person itself, and only the person moves the ticket on, by answering. It is for what cannot be done, never for work that is only hard.

## What it does not check

An evidence entry is proof that something ran. It is not a verdict that the output satisfies the line: that reading is done by whoever reviews the ticket.
