---
title: Serving tofu to another program
description: tofu serve --stdio runs one project for the desk app or a script of your own, over JSON-RPC on standard input and output, with every board, approval, question and status record the app has.
order: 2
updated: 2026-10-22
---

`tofu serve --stdio` runs one project for another program: the desk app, an editor, or a script of
your own. It reads JSON-RPC 2.0 requests on standard input and writes responses and events on
standard output, one JSON object a line, in the protocol `tofu.host/1`. Standard error carries logs
and nothing else. There is no port and no daemon: the program that starts tofu owns it, and closing
standard input stops it.

## Why a frontend needs nothing else

**Everything the app shows is on the wire.** A frontend draws the same conversation, sub-agents,
shells, approvals and quota the terminal app does, from typed events rather than screen text: a turn
ends with `status` `finished`, `stopped` or `failed`, a tool reports its exit code, bytes and
duration as numbers, and a file edit carries its hunks, from the lead or a sub-agent, an
appended write included.

**Every item can be pointed at.** Each event carries a `ref`, the token `Ctrl+R` types for that
item, and `mention.resolve` turns a `ref` back into the item, its speaker and its first line.

**Nothing is decided twice.** The lead asks the frontend the same way it asks you: an approval is a
`tofu/requestApproval` request listing the answers it accepts, and a question with options is a
`tofu/askPerson` request, sent only to a client whose `initialize` declared `questions`. An answer
that isn't listed, or that comes for a request nobody waits on, gets an error reply, and the first
answer wins.

**A long turn keeps running while you look elsewhere.** `session.open` while a turn runs opens the
new session beside it instead of refusing: the first turn goes on, and every event, approval and
status record names the `session` it belongs to. A request that names a `session` goes to that one,
and one that names none means the session opened last, so a client that holds one session sees
nothing change. `status.list` with no `session` answers every open session, and `session.close` lets
one go, refusing while its turn runs unless you pass `stop: true`. Two sessions run turns at once
only with the setting `oneWorkingSessionPerProject` off; while it is on, the default, the second turn ends
`failed` and says the project is busy.

**No window owns a running session.** A session runs in a background holder; `tofu serve` and the
terminal follow it, so closing one leaves the other running, and `session.open` names the holder in
`heldBy`. A second window finds a busy holder wherever its temp folder is; one that cannot reach the
holder says so, names the process, and only follows, running nothing, until that process closes. `turn.send` while a turn runs queues the text as a steer and answers `steered`; pass
`expectedTurnId` to be refused instead. `turn.stop` stops the lead and leaves running sub-agents
working; pass `everything: true` to stop them too. A client that attaches mid-turn gets the
queued messages, pending approvals and working sub-agents at once, and `turn.steer` takes `images: [{path}]` like `turn.send`, so an image pasted while the lead works reaches it. `session.set` from any
client sets the holder's asking and model for every client: the terminal's footer
shows a pick another client made and keeps it, and offers its own default only
to a holder with none. `/loop` and `/cron` made in any client run in the holder,
and the trace row says which process fired them. When the lead forks,
`session.forked` names the new session in `to`, and every client moves to it.

**It answers fast and idles light.** `session.list` answers about 4x and `session.trace` about 2x
faster than before, and the `boardy.*` calls 1.7 to 2x faster on a board of 1,500 tickets, with the
same output. An idle `tofu serve` uses about 0.35% of a core.

## What a frontend can ask

`initialize` answers `capabilities`, one name per family of methods, so a client can tell an older
tofu from a newer one. Beside the session, turn, settings and login methods, the families include:

| Family | Requests | What they answer |
|---|---|---|
| `boards` | `query.session`, `query.usage.history`, `query.limits`, `query.skills`, `query.ledger.summary`, `session.turns` | the fork tree; usage by hour or day with who spent it; each account's burn rate and when a window fills; the skills; a week of decisions per point; one digest per turn with its origin, outcome, held calls, sub-agents, files with line counts, tokens and cost |
| `commands` | `query.commands` | every slash command with its arguments, what it does, whether it runs in tofu or only in the interface, and what a client needs to offer it; `needs` is `turn` or `diff-turn` for `/debug`, `/hunt` and `/simplify`, which start a model turn |
| `memory` | `memory.add`, `memory.edit`, `memory.remove`, `memory.view`, `memory.zoom`, `memory.recall` | entries in the four scopes, and the episode and scope views opened line by line |
| `sessions` | `session.open`, `session.close` | a second session opened beside a running turn, and one let go |
| `side` | `session.branch`, `session.access` | a side chat beside the lead, from its latest point or one you name, with its own model and effort, and what it may write |
| `status` | `status.list`, `status.ack` | every program status record of every open session as it stands, with when it entered its state and the approval or question a blocked one waits on; an acknowledged finished record clears |
| `mention` | `mention.resolve` | the item a `ref` points at |
| `takeover` | `shell.takeover`, `shell.input`, `shell.resize`, `shell.release` | a person borrows a kept shell: every bash command runs in a terminal once the client declares `takeover`, the take-over answers the raw output so far with its colours, keystrokes and sizes go in as base64 bytes, and the release hands it back to the agent with an optional note. Neither kills the process. A refusal is `-32000` with `data.reason`, one of `not_declared`, `unknown_shell`, `not_running`, `not_kept`, `not_in_terminal`, `other_process` and `not_held` |
| `boardy` | `boardy.boards`, `boardy.list`, `boardy.rows` (the list's fields only, 349 KB instead of 11 MB on 1,500 tickets, each row with its `type`, `points`, `reason`, and the `model`, `tools` and `reads` of the sub-agent that worked it), `boardy.get`, `boardy.events`, `boardy.report`, `boardy.create`, `boardy.move`, `boardy.assign`, `boardy.log`, `boardy.hand`, `boardy.triage` | the project's ticket boards, a view of one, a ticket with its events and actuals, a report, and every ticket change the person may make; see [Boardy views](/docs/features/boardy-views) |
| `scratch` | `scratch.list`, `scratch.clean` | the project's scratchpad folders with their size and when each was last touched, and a cleanup with `session`, `cache` and `dryRun` |
| `queries` | `query.context`, `query.drain`, `query.rules`, `query.usage`, `query.settings`, `query.accounts`, `query.ledger` and the other `query.*` | the context items, named down to the rules, memory, skills and tool schemas; where a session's tokens went, as `tofu session drain --json` prints it; rule text and how often each rule fired, quota with each reading's age, each setting with its label, kind, choices and range, every credential with what each API key has spent, its limits and balance; the decisions of one session |

`session.state` lists the approvals waiting, the standing answers, the open questions, the
sub-agents, the kept shells and the cron jobs, which is what a client reads after it falls behind.
Each kept shell carries its port and a `ref` that `mention.resolve` turns back into the call that
started it.

## Events it sends unasked

- `status`, one per program status record: the lead, each sub-agent, each kept shell and each cron
  job, each under its `session`. See [Program status](/docs/features/status).
- `boardy.changed`, `{board, paths}`, within two seconds of a write to a board's files, by tofu or by hand.
- `scratch.changed`, the `scratch.list` answer again, after a `scratch.clean` that removed folders.
- `shell.started` only for a shell tofu keeps, never a one-shot command, and `shell.exited` for
  every one of them. `shell.ready` carries a dev server's port once tofu reads
  `http://localhost:3000` or a like address in its output, colours and all, or finds the shell's own
  process listening, so `npm run dev` gets its port though the command never names one.
- `shell.holder` on every change of who holds a shell, `agent` or `person`, with the release `note`,
  and before `shell.exited` when a held shell ends; `shell.raw`, the raw bytes at an absolute
  `offset`, only while a person holds it. A client that sees a gap in the offsets calls
  `shell.takeover` again for a fresh replay. `shell.started` and `session.state` say `terminal`,
  `holder` and `heldSince`.
- `agent.ended` when a sub-agent ends, with its `state` and the text `report`. On a ticket the
  verdict and each acceptance line ride on the ticket's `reported` entry, read with
  `boardy.get`. A resumed session replays it. `tofu serve --schema` has the full type.
- `turn.account`: the account a turn spends, `picked` as it starts and `moved` off a spent one.
- `turn.started` and every `message.user` carry the images pasted into the message as
  `images: [{n, path, mime}]`, `n` being the `[Image #N]` in its text, live and in history.
- `turn.steerQueued` when another window following the session steers it: the message waits on the
  holder's queue, `item` its id and `text` what was typed, until `turn.steered` says the lead read it.
- `session.turns.updated`: the digest of a turn just ended, the row `session.turns` answers.
- `usage.updated` after each model call, one per turn and agent, with the running `model`,
  `tokensIn`, `tokensOut`, `cacheRead`, `cacheWrite5m`, `cacheWrite1h`, `costUsd` and `decisions`, so
  a client shows what a turn costs as it runs.
- `quota.updated` when a session opens, after each turn, when a turn moves to another account, every
  five minutes, and as soon as a rate-limited account may be asked again. Each window says when it
  was read, whether it is a last good reading kept through a failed poll (`stale`), and the `source`
  and `account_id` it belongs to. See [Subscription quota](/docs/llms/quota).
- `account.state` when an account becomes rate limited or spent, and when it serves again.
- `settings.changed` when a setting changes, through `settings.set`, through `tofu settings set` in
  another window, or by hand in the file, with the new value, the file that changed and where the
  value now comes from.
- `models.changed` when a role is rebound, a sub-agent's model is set or removed, or a model file changes,
  through `tofu models bind`, `tofu agents set`, the app's Settings or by hand: what changed, its name, the
  model it runs now, the scope and the file. The next turn and the next spawn run the new model in every
  window, and a cron that fires with no window open runs it too.
- `memory.scoped` when a remember card is answered: the rule, the scope offered and picked, and by whom.
- `approval.resolved` and `question.resolved`, naming the answer and who sent it; an `allow_always`
  or `reject_always` says `standing`. A standing answer is kept beside its session and read back
  after a restart or `tofu --continue`, never in another session.
- `tool.started` carries the `title` the model gave a bash command, a few words a person reads,
  such as `rebuilt the desk binary`. `tool.completed` carries a typed `summary`: `exit`,
  `durationMs`, `hits` for a search or glob, and lines `added` and `removed` for an edit, so a
  frontend draws a result row without parsing the output.
- `turn.started`, `message.user`, `turn.steered` and `turn.steerQueued` carry a typed `origin`:
  `kind` (`person`, `agent`, `tofu`, `cron`, `hook`, `memory`, `skill`, `library` or `plugin`) and
  `sub`, what exactly sent it, such as `typed`, `steer`, `report` or `fire`. Only a message whose
  kind is `person` is your own words.

## Approvals

A `tofu/requestApproval` carries the agent's own `reason` for the call, cut to one line, and a
`risk`: `low`, `risky`, `destructive` or `unjudged`, from the gate's verdict on it, `unjudged`
when Jev could not answer. Besides allowing
once or always, a client may answer `allow_for`: the call runs, and the same target runs unasked for
an hour, or for `forSeconds` up to a day. `approval.resolved` says `until`. `session.state` lists
every grant still standing, and `approval.revoke` with a `target` ends one at once, sending
`approval.revoked` with who revoked it; a grant that ran out is refused with the reason.

## Trying it

`--cassette` answers every model call from a file, with no network. With `hi.cassette` holding
`{"text":"hi"}`, start tofu and type three lines:

```sh
tofu serve --stdio --cassette hi.cassette
```

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"client":"me"}}
{"jsonrpc":"2.0","id":2,"method":"session.open","params":{}}
{"jsonrpc":"2.0","id":3,"method":"turn.send","params":{"session":"<the id it answered>","text":"say hi"}}
```

tofu answers each request, then streams `turn.started`, `message.delta` and the rest, and ends with
`turn.completed` carrying `status: finished`. A cassette run reads no quota. To try `quota.updated`
and `account.state` with no vendor, point `TOFU_CLAUDE_USAGE_URL` or `TOFU_CODEX_USAGE_URL` at a
usage endpoint of your own, and tofu polls that one instead.

Requests answer typed results, named under `x-requests` in `tofu serve --schema`: `undo` the files
it put back, `label` the row it labelled, `login.start` the account signed in, and `settings.set`
the value with where it now comes from.

## Commands

```
tofu serve --stdio [--dir PATH] [--cassette PATH]
tofu serve --schema
tofu docs serve
tofu docs serve-methods
```

`tofu docs serve` and `tofu docs serve-methods` print the full reference of events and requests.

## Memory

`tofu serve` and `tofu app` run for hours, so they collect garbage about half
as often as a short command: Go's `GOGC` at 200 with a 512 MiB soft memory
limit. Set `GOGC` or `GOMEMLIMIT` in the environment and tofu leaves the
runtime as you set it.
