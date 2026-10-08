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

`tofu docs serve-methods` lists every request, what it takes and what it
answers. This page is what serve sends without being asked.

## Where it lives

- `initialize` first; anything else before it errs. Its `capabilities` name
  each family of methods, so a client tells an older tofu from a newer
- `session.open` replays the chat as events; `session.history` pages it
- `session.listed`: the open row of `session.list` again when a turn starts or
  ends, after a fork and after a rename, so a sidebar never asks.
  `session.updated` follows every turn, a rename and a compaction, with
  `lastAt`, the last record
- `session.settings`: `asking` and the `pick` of wire, model and effort, sent
  to every client when `session.set` changes them
- `origin` on `turn.started` and every `message.user`: `{"kind":"person"}`,
  `{"kind":"cron","job":"c1","schedule":"every 30m"}`,
  `{"kind":"agent","name":"research-1"}` or `{"kind":"tofu","source":"stop
  hook"}`: you, a cron fire, a sub-agent's report, a line tofu added. Older
  sessions, and a cron fire that joined a running turn, read as `person`
- `cron.updated` and `query.cron` carry `live`, `goals` and each job's `id`,
  `schedule`, `prompt`, `paused`, `next` and `ended`
- `turn.steered`: the lead read a message sent mid-turn; until it comes, the
  message is still queued
- `quota.updated`: when a session opens, after every turn and every five
  minutes, with each window's `percent`; `windows: []` means none answered
- `item.persisted`: every line written to `events.jsonl`, with its `logSeq`,
  so history after a seq is a read of the log; a tool call's notice names the
  item of its `tool.started`
- `decision`: every gate, and `tofu/requestApproval`, a request, when a gate
  asks you; `approval.resolved` says which answer won and who sent it
- a slow reader: past 128 queued lines tofu drops, sends `resync`, never
  waits; `session.state` answers it
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
  first answer wins. A `remember` ask also takes `remember_project` or
  `remember_global`, where to keep the memory

`session.set` also takes `wire`, `model` and `effort`, and holds them for
every later turn and cron fire. A `wire` is the source that pays, as the
picker spells it: `claude-sub`, `codex-sub`, `openrouter` or `meta`. One
nobody here is signed in on is refused, and a new `wire` with no `model`
takes that wire's default.

`turn.send` takes the same three, and `images`, a list of `{"path": ...}`.
Each is copied into the session and `[Image #N]` is added to the text. A png,
jpg, gif or webp is taken; anything else refuses the whole send.

A message sent with `turn.steer` while a turn runs waits until the lead's
next step reads it. `turn.sendNow` sends it now: tofu drops the lead's model
request in flight and asks that step again with the message, keeping every
tool result, sub-agent and shell. One message, or every queued one, which is
Esc in the terminal; Esc again, or Esc with nothing queued, is `turn.stop`.
The lead is asked for one line saying what the message changes before its
next tool call, and `tofu session trace` warns on a step that skipped it.

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
