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

- the requests: `initialize`, `session.list`, `session.open`,
  `session.rename`, `turn.send`,
  `turn.steer`, `turn.stop`, `undo`, `shell.read`, `shell.kill`, `label`,
  `settings.set`, `login.start`, `cron.command`, and the reads `query.usage`,
  `query.context`, `query.rules`, `query.agents`, `query.models`,
  `query.ledger`, `query.settings`, `query.library` and `query.cron`
- cron jobs: `cron.command` takes the line you would type, such as
  `/loop 10m check the build` or `/cron pause c1`. `query.cron` answers
  `live`, `goals` and every job with its `id`, `schedule`, `prompt`, `paused`,
  `next` and `ended`, and `cron.updated` carries the same whenever a job is
  added, changed, fired or removed, by you or by the agent
- who started it: `turn.started` and every `message.user`, live or in a
  reopened session's history, carry `origin`. `{"kind":"person"}` is you,
  `{"kind":"cron","job":"c1","schedule":"every 30m"}` a cron fire,
  `{"kind":"agent","name":"research-1"}` a sub-agent's report, and
  `{"kind":"tofu","source":"stop hook"}` a line tofu added itself. A session
  written before this release reads every cron fire as `person`, and a cron
  fire that arrives while a turn runs joins it as a steer, so a reopened
  session reads that one as `person` too
- activity: each `session.list` row and `session.updated` carry `lastAt`,
  the last time anything was recorded in the session. `session.updated`
  follows every turn with the new value
- account quota: `quota.updated` arrives when a session opens, after every
  turn and every five minutes, with each account window's `percent`.
  `windows: []` means no account answered
- the schema: `tofu serve --schema` prints the JSON Schema of every line
  tofu writes, and of every line it reads under `$defs.clientMessage`
- renaming: `session.rename` takes `session`, an id, and `name`, and names
  every generation of that session's family, as `tofu session rename` does.
  `session.updated` follows with the new `name` and `handle`; a name with no
  letter or digit, or a session not here, is refused
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
