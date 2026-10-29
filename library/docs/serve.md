---
topic: serve
title: Serving a project to another program
summary: tofu serve --stdio speaks tofu.host/1, JSON-RPC over standard input and output, for the desk app or any script
verbs: serve
---

## What it is

`tofu serve --stdio` runs one project for another program, the desk app or a script of your own. It reads
JSON-RPC 2.0 requests on standard input and writes responses and events on standard output, one JSON object
a line, in the protocol `tofu.host/1`. Standard error carries logs and nothing else. There is no port and no
daemon: the program that starts tofu owns it. Idle, it uses about 0.35% of a core. `session.list` answers about 4x and `session.trace` about 2x faster than in 0.5.14, and the `boardy.*` calls 1.7 to 2x faster on a board of 1,500 tickets, with the same output.

Every event names its `session`, `turn`, `item`, `seq`, `agent` for a sub-agent's, and `ref`, the token ctrl+r
types for its item. `turn` is the turn that began the lead's run: a follow-up message the lead reads in that run keeps it, and each call still carries its own recorded id in `item`. Events carry data: a turn ends with `status` `finished`, `stopped` or `failed`, a tool
reports exit code, bytes, lines and duration as numbers, and a file edit carries its hunks with three lines of
context. `tofu docs serve-methods` lists every request; this page is what serve sends without being asked.

## Where it lives

- `initialize` first, anything else before it errs; its `capabilities` name each family of methods
- `session.open` replays the chat as events; `session.history` pages it back through every earlier generation of the family, to the first message. Opened while a turn runs, it keeps
  that turn going in its own session (capability `sessions`), each event naming its `session`; a request
  with no `session` means the session opened last, and `session.close` lets one go. Both run turns at once
  only with `oneWorkingSessionPerProject` off; while on, the default, the second turn ends `failed`. Every window joins the project's `tofu host` as a member, and the session runs there
- `session.listed`: the open row of `session.list` again when a turn starts or ends, after a fork and a
  rename; `session.updated` follows every turn, a rename (by `tofu session rename` in another process too) and a compaction, with `lastAt`, the last record;
  `session.turns.updated` carries the ended turn's `digest`, one row of `session.turns`
- `turn.account`: the account a turn spends, `source`, `account_id`, `login`, `model` and `reason`:
  `picked` as it starts, `moved` with `from_account` when tofu leaves a spent account mid-turn
- `session.settings`: `asking` and the `pick` of wire, model and effort, to every client on `session.set`
- `origin` on `turn.started`, every `message.user`, `turn.steered` and `turn.steerQueued`: `kind` (`person`,
  `agent`, `tofu`, `cron`, `hook`, `memory`, `skill`, `library`, `plugin`) and `sub`, what sent it, such as
  `{"kind":"person","sub":"typed"}` or `{"kind":"cron","sub":"fire","job":"c1","schedule":"every 30m"}`;
  only `person` is your words. `images: [{n, path, mime}]`, `n` the `[Image #N]` in the text, also on a steer. A message carries `sentAt`, when you sent it, and a note or a reply its `at`
- `cron.updated` and `query.cron`: `live`, `goals` and each job's `id`, `schedule`, `prompt`, `paused`, `next`, `ended`, `lastFired`, `fires`, `lastResult`, `expires` and `until`; every member hears `cron.updated` when a job fires and again when its result is in.
  A fire that starts a turn shows in its `origin` alone, one joining a running turn sends a `note`
- `agent.updated`: a sub-agent's changed fields, `moves` among them, each `{agent, move, round, reason}` with `move` `spawned`, `reviewed`, `reopened` or `accepted`, drawn on the terminal sub-agent card's `review` line; `context.updated` with `agent` set is that sub-agent's own context, apart from the lead's
- `tool.started`, `tool.completed` and `file.edit` (an append as an added hunk) by a sub-agent carry its
  `agent` and `instance`, live and replayed; `tool.started` the bash `title` the model gave, and
  `tool.completed` a `summary`: `exit`, `durationMs`, `hits` for a search, `added` and `removed` lines. Both carry `card`, the call drawn as a row (`family`, `doing`, `verb`, `object`, `meta`, `tone`, and on a ticket move `from` and `to`), its `group`, and `groupSummary`, the turn's group row: `calls`, `failed`, its `head` and its `open` lines
- `agent.ended`: `endedAt`, `durationMs`, the `turn` that spawned it;
  `turn.steered`: the lead read a message sent mid-turn, until then queued; `usage.updated`, per turn and agent after each call, the running `tokensIn`, `tokensOut`, `cacheRead`, `cacheWrite5m`, `cacheWrite1h`, `costUsd`, `decisions`
- `quota.updated`: on open, after each turn, on an account move, every five minutes, and once a `retry_at`
  passes; each window's `percent`, `source`, `account_id` (as `query.accounts`), `read_at` and `stale`, true
  for a last good reading kept through a failed poll; `windows: []`, none answered
- `account.state` `{source, account_id, state, retry_at}`: an account turned `rate_limited` or `spent`,
  or back to `serving`; one serving from the start says nothing
- `settings.changed` `{key, value, scope, source, by}`: a setting changed by `settings.set`, by `tofu settings
  set` in another process or by hand; `scope` is the file that changed, `source` where the value now
  resolves from, `default` once the key left both files, and `by` who made a `tofu settings set` that ran while the host was up: `person`, the `--as` name, or the sub-agent's session
- `models.changed` `{what, name, model, scope, file}`, one per change to a role file, `agent-models.yaml` or a
  model file, from any process or by hand: `what` is `role`, `agent` or `model`, and `model` is empty once
  unbound; every client and the session's holder read it before the next turn and the next spawn
- `item.persisted`: every line of `events.jsonl` with its `logSeq`; a tool call's names its `tool.started`
- `decision`: every gate, with `at` and `call`, the `tool.started` it judged, and the classifier that judged it: `provider`, `model`, the build the answer reported, and `durationMs`; its ledger row holds the same
- `memory.scoped`: a `remember` kept: `statement`, `offered`, `picked` (`none` for no), `by` (`person`, `auto`); `memory.changed` `{scope, file}`: a memory entry file added, changed or removed, by tofu, another process or by hand, within a second
- `tofu/askPerson`, a request, when the lead asks a question with options, only to a client whose `initialize`
  declared `questions`; any other is `undelivered` at once. Answer `{"outcome":"submitted","answers":[{"id":
  "lib","chosen":["resty"],"text":""}]}` or `cancelled`; `question.resolved` says how and by whom. `tofu docs asking`
- `shell.started`: only a kept shell, never a one-shot, with `kept` (`background` or `moved`), `dir`, `port`,
  `ready`, `leftOver` and `ref`, the quote of the call that started it; every one ends in `shell.exited` with
  `endedAt`, and `exitCode` when tofu saw it. `shell.ready` `{shell, port}` follows once tofu reads a local
  address in its output, colours stripped, or finds its process listening ten seconds after it started
- `status`: what the lead, each sub-agent, shell and cron job is doing, as OSC 7501 records with `at`, when
  the state began, and `ask` on a blocked one; `status.list` answers them all with their `session`. `tofu docs status`
- `boardy.changed` `{board, paths, by}` within two seconds of a write to a board's files, `by` naming who made a `tofu boardy` write that ran while the host was up, as on `settings.changed`; a `boardy.rows` row
  carries `type`, `points`, `reason`, and the `model`, `tools` and `reads` of the sub-agent that worked it;
  `scratch.changed`, the `scratch.list` answer again, after a `scratch.clean` that removed a folder
- a slow reader: past 128 queued lines tofu drops, sends `resync`, never waits; `session.state` answers it
- `tofu serve --schema`: the JSON Schema of every line out, in under `$defs.clientMessage`

## Change it

Confirmations follow the setting `gatePrompt`, auto unless you changed it. `session.open` and `session.set`
take `asking`, `ask` or `auto`, for that session alone. Every gate sends a `decision` event either way:

- `auto`: a call jev would ask about runs unasked, and one the gate could not judge is refused.
  `tofu/requestApproval` still comes, and the turn waits, for a change to tofu's settings or a harness
  file, a PreToolUse hook that asks, untrusted project hooks, `remember` and `rule_override`
- `ask`: those, and every call jev would ask about or could not judge
- a request carries the agent's `reason` and a `risk`: `low`, `risky`, `destructive`, or `unjudged` when
  jev could not answer. Its `decisions` are the answers it takes: `allow_once`, `allow_for` (an hour, or
  `forSeconds` up to a day), `allow_always`, `reject_once`, `reject_always`, `cancelled` (stops the turn),
  or for `remember` where to keep it. Any other answer, or one nothing waits on, errs; the first wins
- `allow_for`, `allow_always` and `reject_always` stand for that target in that session, read back after a
  restart or `--continue`: `approval.resolved` says `standing` and `until`, `session.state` lists them, and
  `approval.revoke` `{target}` ends one at once, sending `approval.revoked` with who revoked it
- a turn a cron job started has nobody to ask: what would ask is refused, saying no person, and nothing
  waits. `turn.stop` with `lead: true` withdraws a waiting approval, which resolves `cancelled`

`session.set` also takes `wire`, `model` and `effort`, held for every later turn and cron fire. A `wire` is
the source that pays: `claude-sub`, `codex-sub`, `openrouter` or `meta`. One nobody is signed in on is
refused; a new `wire` with no `model` takes its default. `turn.send` takes the same three, and `images`
`{"path": ...}`, kept as `[Image #N]`. Opening a side chat picks the model it was branched with.

A `turn.steer` message waits for the lead's next step. `turn.sendNow` sends it now: tofu drops the model
request in flight and asks that step again with it, keeping every tool result, sub-agent and shell; Esc in
the terminal, where Esc again is `turn.stop`. The lead says in one line what it changes before its next
tool call, and `tofu session trace` warns on a skip.

## Check it

    tofu serve --stdio --cassette hi.cassette

`--cassette` answers every model call as `tofu drive` does, with no network. With `hi.cassette` holding
`{"text":"hi"}`, type:

    {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"client":"me"}}
    {"jsonrpc":"2.0","id":2,"method":"session.open","params":{}}
    {"jsonrpc":"2.0","id":3,"method":"turn.send","params":{"session":"<the id it answered>","text":"say hi"}}

tofu answers each request, then streams `turn.started`, `message.delta` and the rest, and ends with
`turn.completed` carrying `status: finished`. A cassette run reads no quota: set `TOFU_CLAUDE_USAGE_URL` or
`TOFU_CODEX_USAGE_URL` to a usage endpoint of your own to see `quota.updated` and `account.state`.

## Undo it

Close standard input: tofu stops every running turn, keeps the sessions, and exits.
