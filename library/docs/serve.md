---
topic: serve
title: Serving a project to another program
summary: tofu serve --stdio speaks tofu.host/1, JSON-RPC over standard input and output, for the desk app or any script
verbs: serve
---

## What it is

`tofu serve --stdio` runs one project for another program, the desk app or
a script of your own. It reads JSON-RPC 2.0 requests on standard input and
writes responses and events on standard output, one JSON object a line, in
the protocol `tofu.host/1`. Standard error carries logs and nothing else.
There is no port and no daemon: the program that starts tofu owns it.

Every event names its `session`, `turn`, `item` and `seq`, and `agent` when a
sub-agent produced it. Events carry data, not sentences: a turn ends with
`status` `finished`, `stopped` or `failed`, a tool reports its exit code,
bytes, lines and duration as numbers, and a file edit carries its hunks with
three lines of context.

## Where it lives

- the requests: `initialize`, `session.list`, `session.open`, `turn.send`,
  `turn.steer`, `turn.stop`, `undo`, `shell.read`, `shell.kill`, `label`,
  `settings.set`, `login.start`, and the reads `query.usage`,
  `query.context`, `query.rules`, `query.agents`, `query.models`,
  `query.ledger`, `query.settings` and `query.library`
- the schema: `tofu serve --schema` prints the JSON Schema of every line
  tofu writes, and of every line it reads under `$defs.clientMessage`
- a session another tofu holds: `session.open` answers with the error
  `session.busy`, carrying the process that holds it
- the log: every line tofu writes to the session's `events.jsonl` is
  announced as `item.persisted`, with its `logSeq`, so history after a seq
  is a read of the log. A tool call's notice names the same item as its
  `tool.started`
- a slow reader: tofu never waits on it. Past 128 queued lines it drops
  what it cannot hold and sends `resync`, and the reader asks again for
  what it missed

## Change it

Confirmations follow the setting `gatePrompt`, auto unless you changed it.
`session.open` takes `asking`, `ask` or `auto`, for that session alone:

- `auto`: no request is sent; every gate still sends a `decision` event
- `ask`: a gate jev would ask about sends `tofu/requestApproval` as a
  request, and the turn waits. Answer with `allow_once`, `allow_always`,
  `reject_once`, `reject_always`, or `cancelled`, which stops the turn. The
  first answer wins, and `approval.resolved` says which it was

`--cassette PATH` answers every model call from a recorded cassette, as
`tofu drive` does, so a frontend is built and tested with no model and no
network.

## Check it

    tofu serve --stdio --cassette hi.cassette

with `hi.cassette` holding `{"text":"hi"}`, then type:

    {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"client":"me"}}
    {"jsonrpc":"2.0","id":2,"method":"session.open","params":{}}
    {"jsonrpc":"2.0","id":3,"method":"turn.send","params":{"session":"<the id it answered>","text":"say hi"}}

tofu answers each request, then streams `turn.started`, `message.delta` and
the rest, and ends with `turn.completed` carrying `status: finished`.

## Undo it

Close standard input: tofu stops the running turn, writes what it still
holds, and exits. The session stays on disk like any other, and
`tofu session list` shows it.
