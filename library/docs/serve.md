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

- sessions: `session.list`, `session.open`, `session.state`, `session.set`,
  `session.rename`, `session.compact`, `session.history`. Turns: `turn.send`,
  `turn.steer`, `turn.unsteer`, `turn.stop`, `undo`. Shells: `shell.run`,
  `shell.read`, `shell.kill`. Others: `initialize`, `label`, `settings.set`,
  `login.start`, `cron.command`. Reads, which answer out of order as
  `session.list`, `session.history` and `shell.run` do, so none holds
  `turn.stop`: `query.usage`, `query.context`, `query.rules`, `query.agents`,
  `query.models`, `query.ledger`, `query.settings`, `query.library`, `query.cron`
- `query.ledger`: takes `id`, or `last` and `point`; answers `rows`, each a
  ledger row with its `precedents` as `tofu why --json` prints it, or `[]`
- `turn.stop` with `lead: true` stops the lead alone. `turn.unsteer` takes back
  a queued steer by its `text` and answers `removed`
- `shell.run`: runs `command` as `!` does and answers `output` and `stopped`;
  the next turn reads it. Never mid-turn, one at a time, and `turn.stop` stops it
- `session.compact`: answers `results`, `tokensBefore`, `tokensAfter` and
  `into` when anything shrank, which `session.updated` then names
- `session.history`: takes `session`, `limit` and `before`; answers `lines`,
  each `{method, params}` as `session.open` sends it, from `first` of `total`.
  `session.open` takes `replay`, how many of the newest it sends, and on a
  session another tofu holds errs `session.busy`, naming the process
- `initialize`: answers `capabilities`, one name a family of methods (`list`,
  `ledger`, `history` and so on), so a client tells an older tofu from a newer
- `session.list`: takes `search` and `limit`, both optional; one row a
  session with `id`, `name`, `handle`, `task`, `turns`, `lastAt`, `wire`,
  `model`, `costUsd`, `open` for the one open here, `running` while a turn
  runs in it, and `heldBy` when another tofu holds it. `session.listed`
  sends the open row again when a turn starts or ends, after a fork and after
  a rename, so a sidebar never asks
- `session.state`: everything a client that missed lines or opened mid-turn
  needs: `running` and its `turn`, `asking` (absent while `gatePrompt`
  decides), the `pick` of wire, model and effort, waiting approvals,
  sub-agents, shells, the context window and the cron jobs. It answers `resync`
- `session.rename`: takes `session` and `name` and names the whole family, as
  `tofu session rename` does; `session.updated` follows. A name with no letter
  or digit, or a session not here, is refused
- cron: `cron.command` takes the line you would type, such as `/loop 10m check
  the build`. `query.cron` and `cron.updated` carry `live`, `goals` and each
  job's `id`, `schedule`, `prompt`, `paused`, `next` and `ended`
- `origin` on `turn.started` and every `message.user`: `{"kind":"person"}`,
  `{"kind":"cron","job":"c1","schedule":"every 30m"}`,
  `{"kind":"agent","name":"research-1"}` or `{"kind":"tofu","source":"stop
  hook"}`: you, a cron fire, a sub-agent's report, a line tofu added. Older
  sessions, and a cron fire that joined a running turn, read as `person`
- `lastAt`, the last record, is on `session.list`, `session.listed` and
  `session.updated`, which follows every turn
- `quota.updated`: when a session opens, after every turn and every five
  minutes, with each window's `percent`; `windows: []` means none answered
- `item.persisted`: every line written to `events.jsonl`, with its `logSeq`,
  so history after a seq is a read of the log; a tool call's notice names the
  item of its `tool.started`
- a slow reader: past 128 queued lines tofu drops, sends `resync`, never waits
- `tofu serve --schema` prints the JSON Schema of every line written, and of
  every line read under `$defs.clientMessage`

## Change it

Confirmations follow the setting `gatePrompt`, auto unless you changed it.
`session.open` and `session.set` take `asking`, `ask` or `auto`, for that
session alone:

- `auto`: no request is sent; every gate still sends a `decision` event
- `ask`: a gate jev would ask about sends `tofu/requestApproval` as a
  request, and the turn waits. Answer with `allow_once`, `allow_always`,
  `reject_once`, `reject_always`, or `cancelled`, which stops the turn. The
  first answer wins, and `approval.resolved` says which it was

`session.set` also takes `wire`, `model` and `effort`, and holds them for
every later turn and cron fire; `session.settings` tells every client. A
`wire` is the source that pays, as the picker spells it: `claude-sub`,
`codex-sub`, `openrouter` or `meta`. One nobody here is signed in on is
refused, and a new `wire` with no `model` takes that wire's default.

`turn.send` takes the same three, and `images`, a list of `{"path": ...}`.
Each is copied into the session and `[Image #N]` is added to the text. A png,
jpg, gif or webp is taken; anything else refuses the whole send.

`--cassette PATH` answers every model call from a recorded cassette, as
`tofu drive` does, so a frontend is built with no model and no network.

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
