---
title: Serving tofu to another program
description: tofu serve --stdio runs one project for the desk app or a script of your own, over JSON-RPC on standard input and output, with every board, approval, question and status record the app has.
order: 2
updated: 2026-10-09
---

`tofu serve --stdio` runs one project for another program: the desk app, an
editor, or a script of your own. It reads JSON-RPC 2.0 requests on standard
input and writes responses and events on standard output, one JSON object a
line, in the protocol `tofu.host/1`. Standard error carries logs and nothing
else. There is no port and no daemon: the program that starts tofu owns it,
and closing standard input stops it.

## Why a frontend needs nothing else

**Everything the app shows is on the wire.** A frontend draws the same
conversation, sub-agents, shells, approvals and quota the terminal app
does, from typed events rather than screen text: a turn ends with `status`
`finished`, `stopped` or `failed`, a tool reports its exit code, bytes and
duration as numbers, and a file edit carries its hunks.

**Every item can be pointed at.** Each event carries a `ref`, the token
`Ctrl+R` types for that item, and `mention.resolve` turns a `ref` back into
the item, its speaker and its first line.

**Nothing is decided twice.** The lead asks the frontend the same way it
asks you: an approval is a `tofu/requestApproval` request listing the
answers it accepts, and a question with options is a `tofu/askPerson`
request, sent only to a client whose `initialize` declared `questions`. An
answer that isn't listed, or that comes for a request nobody waits on, gets
an error reply, and the first answer wins.

## What a frontend can ask

`initialize` answers `capabilities`, one name per family of methods, so a
client can tell an older tofu from a newer one. Beside the session,
turn, settings and login methods, the families include:

| Family | Requests | What they answer |
|---|---|---|
| `boards` | `query.session`, `query.usage.history`, `query.limits`, `query.skills`, `query.ledger.summary` | the fork tree; usage by hour or day with who spent it; each account's burn rate and when a window fills; the skills; a week of decisions per point |
| `memory` | `memory.add`, `memory.edit`, `memory.remove`, `memory.view`, `memory.zoom`, `memory.recall` | entries in the four scopes, and the episode and scope views opened line by line |
| `side` | `session.branch`, `session.access` | a side chat beside the lead, and what it may write |
| `status` | `status.list` | every program status record as it stands |
| `mention` | `mention.resolve` | the item a `ref` points at |
| `queries` | `query.context`, `query.rules`, `query.usage` and the other `query.*` | the context items, rule text and how often each rule fired, quota with each reading's age |

`session.state` lists the approvals waiting, the standing answers, the
open questions, the sub-agents, the kept shells and the cron jobs, which is
what a client reads after it falls behind.

## Events it sends unasked

- `status`, one per program status record: the lead, each sub-agent, each
  kept shell and each cron job. See [Program status](/docs/features/status).
- `shell.started` only for a shell tofu keeps, never a one-shot command,
  and `shell.exited` for every one of them.
- `quota.updated` when a session opens, after each turn and every five
  minutes, with each reading's age. See [Subscription
  quota](/docs/llms/quota).
- `memory.scoped` when a remember card is answered: the rule, the scope
  offered, the scope picked and who picked it.
- `approval.resolved` and `question.resolved`, naming the answer and who
  sent it; an `allow_always` or `reject_always` says `standing`. A standing
  answer is kept beside its session and read back after a restart or
  `tofu --continue`, never in another session.

## Trying it

`--cassette` answers every model call from a file, with no network. With
`hi.cassette` holding `{"text":"hi"}`, start tofu and type three lines:

```sh
tofu serve --stdio --cassette hi.cassette
```

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"client":"me"}}
{"jsonrpc":"2.0","id":2,"method":"session.open","params":{}}
{"jsonrpc":"2.0","id":3,"method":"turn.send","params":{"session":"<the id it answered>","text":"say hi"}}
```

tofu answers each request, then streams `turn.started`, `message.delta`
and the rest, and ends with `turn.completed` carrying `status: finished`.

## Commands

```
tofu serve --stdio [--dir PATH] [--cassette PATH]
tofu serve --schema
tofu docs serve
tofu docs serve-methods
```

`tofu serve --schema` prints the JSON Schema of every line in and out, with
each method's `params` and `result` under `x-requests`. `tofu docs serve`
and `tofu docs serve-methods` print the full reference of events and
requests.
