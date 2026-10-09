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

Every event names its `session`, `turn`, `item`, `seq`, `agent` for a
sub-agent's, and `ref`, the token ctrl+r types for its item. Events carry
data: a turn ends with `status` `finished`, `stopped` or `failed`, a tool
reports exit code, bytes, lines and duration as numbers, and a file edit
carries its hunks with three lines of context.

`tofu docs serve-methods` lists every request, what it takes and what it
answers. This page is what serve sends without being asked.

## Where it lives

- `initialize` first; anything else before it errs. Its `capabilities` name
  each family of methods, so a client tells an older tofu from a newer
- `session.open` replays the chat as events; `session.history` pages it
- `session.listed`: the open row of `session.list` again when a turn starts or
  ends, after a fork and a rename; `session.updated` follows every turn, a
  rename and a compaction, with `lastAt`, the last record
- `session.settings`: `asking` and the `pick` of wire, model and effort, sent
  to every client when `session.set` changes them
- `origin` on `turn.started` and every `message.user`: `{"kind":"person"}`,
  `{"kind":"cron","job":"c1","schedule":"every 30m"}`,
  `{"kind":"agent","name":"research-1"}` or `{"kind":"tofu","source":"stop
  hook"}`: you, a cron fire, a sub-agent's report, a line tofu added. Older
  sessions, and a cron fire that joined a running turn, read as `person`
- `cron.updated` and `query.cron`: `live`, `goals` and each job's `id`,
  `schedule`, `prompt`, `paused`, `next` and `ended`; a fire that starts a
  turn shows in its `origin` alone, one joining a running turn sends a `note`
- `tool.started`, `tool.completed` and `file.edit` by a sub-agent carry its
  `agent` and `instance`, live and on a replay, which keeps each event's real
  `turn` and item
- `agent.ended`: `endedAt`, `durationMs` and the `turn` that spawned it, as
  recorded, so a replay matches live
- `turn.steered`: the lead read a message sent mid-turn, until then queued
- `quota.updated`: on open, after each turn and every five minutes, each
  window's `percent`; `windows: []`, none answered
- `item.persisted`: every line of `events.jsonl` with its `logSeq`, so history
  after a seq is a read of the log; a tool call's names its `tool.started`
- `decision`: every gate, with `at` and `call`, the item of the
  `tool.started` it judged; `tofu/requestApproval`, a request, when a gate
  asks you, and `approval.resolved`, which answer won and who sent it
- `tofu/askPerson`, a request, when the lead asks a question with options,
  only to a client whose `initialize` declared `questions`; any other is
  `undelivered` at once. Answer `{"outcome":"submitted","answers":[{"id":
  "lib","chosen":["resty"],"text":""}]}` or `cancelled`; `question.resolved`
  says how and by whom. `tofu docs asking`
- `shell.started`: only a kept shell, never a one-shot, with `kept`
  (`background` or `moved`), `dir`, `port`, `ready` and `leftOver`; every one
  ends in `shell.exited` with `endedAt`, and `exitCode` when tofu saw it
- `status`: what the lead, each sub-agent, shell and cron job is doing, as
  OSC 7501 records; `status.list` answers them all. `tofu docs status`
- a slow reader: past 128 queued lines tofu drops, sends `resync`, never
  waits; `session.state` answers it
- `tofu serve --schema`: the JSON Schema of every line out, and in under
  `$defs.clientMessage`

## Change it

Confirmations follow the setting `gatePrompt`, auto unless you changed it.
`session.open` and `session.set` take `asking`, `ask` or `auto`, for that
session alone:

- `auto`: no request is sent; every gate still sends a `decision` event
- `ask`: a gate jev would ask about sends `tofu/requestApproval` as a
  request, and the turn waits. Answer with `allow_once`, `allow_always`,
  `reject_once`, `reject_always`, or `cancelled`, which stops the turn. The
  first answer wins. A `remember` ask takes `remember_` plus where to keep
  it (`global`, `user_local`, `project`, `project_local`), or a reject

`session.set` also takes `wire`, `model` and `effort`, and holds them for
every later turn and cron fire. A `wire` is the source that pays, as the
picker spells it: `claude-sub`, `codex-sub`, `openrouter` or `meta`. One
nobody here is signed in on is refused, and a new `wire` with no `model`
takes that wire's default.

`turn.send` takes the same three, and `images`, a list of `{"path": ...}`:
a png, jpg, gif or webp, copied into the session as `[Image #N]`.

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
holds, and exits. The session stays on disk; `tofu session list` shows it.
