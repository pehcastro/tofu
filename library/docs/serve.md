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
daemon: the program that starts tofu owns it.

Every event names its `session`, `turn`, `item`, `seq`, `agent` for a sub-agent's, and `ref`, the token ctrl+r
types for its item. Events carry data: a turn ends with `status` `finished`, `stopped` or `failed`, a tool
reports exit code, bytes, lines and duration as numbers, and a file edit carries its hunks with three lines of
context. `tofu docs serve-methods` lists every request; this page is what serve sends without being asked.

## Where it lives

- `initialize` first, anything else before it errs; its `capabilities` name each family of methods
- `session.open` replays the chat as events; `session.history` pages it. Opened while a turn runs, it keeps
  that turn going in its own session (capability `sessions`), each event naming its `session`; a request
  with no `session` means the session opened last, and `session.close` lets one go. Both run turns at once
  only with `oneTurnPerProject` off; while it is on, the default, the second turn ends `failed`: project busy
- `session.listed`: the open row of `session.list` again when a turn starts or ends, after a fork and a
  rename; `session.updated` follows every turn, a rename and a compaction, with `lastAt`, the last record;
  `session.turns.updated` carries the ended turn's `digest`, one row of `session.turns`
- `turn.account`: the account a turn spends, `source`, `account_id`, `login`, `model` and `reason`:
  `picked` as it starts, `moved` with `from_account` when tofu leaves a spent account mid-turn
- `session.settings`: `asking` and the `pick` of wire, model and effort, to every client on `session.set`
- `origin` on `turn.started` and every `message.user`: `{"kind":"person"}`, `{"kind":"cron","job":"c1",
  "schedule":"every 30m"}`, `{"kind":"agent","name":"research-1"}` or `{"kind":"tofu","source":"stop hook"}`:
  you, a cron fire, a sub-agent's report, a line tofu added. Older sessions, and a cron fire that joined a
  running turn, read as `person`
- `cron.updated` and `query.cron`: `live`, `goals` and each job's `id`, `schedule`, `prompt`, `paused`, `next`
  and `ended`; a fire that starts a turn shows in its `origin` alone, one joining a running turn sends a `note`
- `tool.started`, `tool.completed` and `file.edit` by a sub-agent carry its
  `agent` and `instance`, live and replayed, with their real `turn` and item
- `agent.ended`: `endedAt`, `durationMs`, the `turn` that spawned it;
  `turn.steered`: the lead read a message sent mid-turn, until then queued
- `quota.updated`: on open, after each turn, when a turn moves account, every five minutes, and as soon as a
  rate-limited account's `retry_at` passes; each window's `percent`, `source`, `account_id` (the id
  `query.accounts` uses), `read_at` and `stale`, true for a last good reading kept through a failed poll;
  `windows: []`, none answered
- `account.state` `{source, account_id, state, retry_at}`: an account turned `rate_limited` or `spent`,
  or back to `serving`; one serving from the start says nothing
- `settings.changed` `{key, value, scope, source}`: a setting changed by `settings.set`, by `tofu settings
  set` in another process or by hand; `scope` is the file that changed, `source` where the value now
  resolves from, `default` once the key left both files
- `item.persisted`: every line of `events.jsonl` with its `logSeq`, so history
  after a seq is a read of the log; a tool call's names its `tool.started`
- `decision`: every gate, with `at` and `call`, the `tool.started` it judged
- `memory.scoped`: a `remember` kept or answered: `statement`, `offered` (Jev's
  pick, else the lead's), `picked` (`none` for no), `by` (`person`, `auto`)
- `tofu/askPerson`, a request, when the lead asks a question with options, only to a client whose `initialize`
  declared `questions`; any other is `undelivered` at once. Answer `{"outcome":"submitted","answers":[{"id":
  "lib","chosen":["resty"],"text":""}]}` or `cancelled`; `question.resolved` says how and by whom. `tofu docs asking`
- `shell.started`: only a kept shell, never a one-shot, with `kept` (`background` or `moved`), `dir`, `port`,
  `ready`, `leftOver` and `ref`, the quote of the call that started it; every one ends in `shell.exited` with
  `endedAt`, and `exitCode` when tofu saw it. `shell.ready` `{shell, port}` follows once tofu reads a local
  address in its output, colours stripped, or finds its process listening ten seconds after it started
- `status`: what the lead, each sub-agent, shell and cron job is doing, as OSC 7501 records with `at`, when
  the state began, and `ask` on a blocked one; `status.list` answers them all with their `session`. `tofu docs status`
- `boardy.changed` `{board, paths}` within two seconds of a write to a board's files, by tofu or by
  hand; `scratch.changed`, the `scratch.list` answer again, after a `scratch.clean` that removed
- a slow reader: past 128 queued lines tofu drops, sends `resync`, never waits; `session.state` answers it
- `tofu serve --schema`: the JSON Schema of every line out, in under `$defs.clientMessage`

## Change it

Confirmations follow the setting `gatePrompt`, auto unless you changed it. `session.open` and `session.set`
take `asking`, `ask` or `auto`, for that session alone. Every gate sends a `decision` event either way:

- `auto`: a call jev would ask about runs unasked, and one the gate could not judge is refused.
  `tofu/requestApproval` still comes, and the turn waits, for a change to tofu's settings or a harness
  file, a PreToolUse hook that asks, untrusted project hooks, `remember` and `rule_override`
- `ask`: those, and every call jev would ask about or could not judge
- a request's `decisions` are the answers it takes: `allow_once`,
  `allow_always`, `reject_once`, `reject_always`, `cancelled` (stops the
  turn), or for `remember` where to keep it. Any other answer, or one to an
  id nothing waits on, gets an error reply. The first answer wins, and
  `approval.resolved` names it and who sent it
- `allow_always` and `reject_always` stand for that target in that session,
  read back after a restart or `--continue`, never in another session:
  `approval.resolved` says `standing`, `session.state` lists them, and each
  call one decides unasked is a `standing` row in `tofu why`
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

`--cassette` answers every model call as `tofu drive` does, with no network; with `hi.cassette` holding
`{"text":"hi"}`, type:

    {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"client":"me"}}
    {"jsonrpc":"2.0","id":2,"method":"session.open","params":{}}
    {"jsonrpc":"2.0","id":3,"method":"turn.send","params":{"session":"<the id it answered>","text":"say hi"}}

tofu answers each request, then streams `turn.started`, `message.delta` and the rest, and ends with
`turn.completed` carrying `status: finished`. A cassette run reads no quota: to see `quota.updated` and
`account.state` with no vendor, set `TOFU_CLAUDE_USAGE_URL` or `TOFU_CODEX_USAGE_URL` to a usage endpoint of
your own.

## Undo it

Close standard input: tofu stops every running turn, keeps the sessions, and exits.
