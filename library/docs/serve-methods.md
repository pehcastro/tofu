---
topic: serve-methods
title: Every method tofu serve answers
summary: the requests of tofu.host/1, what each takes and what it answers, for a frontend wiring tofu serve
verbs: serve
---

## What it is

Every request `tofu serve --stdio` answers; `tofu docs serve` says how to start it, `tofu serve --schema`
carries every field. Reads, reloads, the `session.*` and `memory.*` reads, `mention.resolve`, `shell.run`,
`learn.scan`, `setup.check` and the logins answer out of order, so none holds `turn.stop`. Every request
answers a typed result with no envelope, and a verb that fails errs with its problems.

## Where it lives

- `initialize`: takes `client`, `versions` and `capabilities`, where `questions` says it answers
  `tofu/askPerson`; answers `protocol`, `tofu`, `project` and `capabilities`, one name a family of methods
- `session.list`: takes `search`, `limit` and `kind`; answers `head` and `sessions`, each `id`, `name`,
  `handle`, `task`, `turns`, `lastAt`, `wire`, `model`, `costUsd`, `outcome`, `open` (here), `running`,
  `members` (every tofu, desk and serve joined to it) and `kind`, `main` or `side`; a side chat adds `parent`, `owns`, `preset`
- side chats (capability `side`): `session.branch` takes `session`, `kind: "side"`, `seed` (`summary` or
  `none`), `owns` or `preset`, `name`, `model`, `effort` and `at` `{item}`, the parent's event to branch from
  (the `item` `mention.resolve` answers), whose summary stops there; answers `session`, `handle`, `parent`
  `{session, event}`, `owns`, `preset`, `carried`, `model` and `effort`; `session.open` of the side chat
  picks its `model`. `tofu session branch` takes `--model`, `--effort`, `--at`. `session.access` takes `session`
  and `owns` or `preset`, `read` with neither, and errs while that chat's turn runs
- `session.open`: takes `session` (none starts a fresh one), `asking` and `replay`, how many of the newest
  lines it sends; answers `session`, `fresh` and `members`, each `{process, kind, joined}` with `kind` `tui`, `desk` or `serve`: the session runs in the project's `tofu host`, every client joins it as a member, replaying the session and then streaming its events live, and `turn.send`, `turn.stop` and answers run there. `session.member` tells every client the new `members` whenever one joins or leaves, and `session.state` carries them too. When the last member leaves, the host cancels the session's turn, naming that member, and ends its shells. While a
  turn runs, the new one opens beside it (capability `sessions`) and one already open here takes the focus;
  a request with no `session` means the one opened last. `session.close` takes `session` and `stop`, and
  errs while that session's turn runs unless `stop` is true, and on the only session open
- `session.state`, the answer to `resync`: `running` and its `turn`, `asking`, the `pick`, waiting approvals,
  `standing` answers (`target`, `decision`; kept beside the session, read back when it reopens, carried into
  its compaction and never into another session), open `questions`, sub-agents, kept shells (each with `port`
  once tofu learns it and `ref`, the quote of the call that started it), the context and the cron jobs
- `session.set`: takes `asking`, `wire`, `model` and `effort`, held for later turns and cron fires; `session.settings` tells every client
- `session.rename`: takes `session` and `name`, and names the whole family
- `session.compact`: answers `results`, `tokensBefore`, `tokensAfter`, `into`, and errs on a session opened
  beside the first. `session.history`: takes `session`, `limit` and `before`; answers `lines`, each
  `{method, params}` as `session.open` sends it, from `first` of `total`
- `session.info` and `session.trace` answer what `tofu session info` and `trace` print, `session.info` with `carried_from` `{session, handle, messages}` when it began with messages carried over. `session.find` takes
  `session` and any of `tool`, `command`, `file`, `text`, `agent`, `since`, `until`; answers `handle`, `query`, `hits`
- `session.turns` takes `session`; answers `snapshots`, how many turns undo holds, and `turns`, each
  `turn`, `at`, `ended_at`, `worked_for_ms`, `origin`, `asked`, `summary` (the lead's last reply),
  `status`, `outcome` (`loop_guard`, `step_cap` and the rest), `error`, `held` (calls a `gate` or a
  `hook` refused), `agents`, `files` (`path`, `op`, `added`, `removed`, `agent`), `calls`, `tokens`
  (`input`, `output`, `cache_read`) and `cost_usd`. `session.turns.updated` sends one when a turn ends
- `turn.send`: takes `session`, `text`, `mentions` (each a `ref`, kept where `text` has it, else added at the
  end; a path gains `@`), `images` (png, jpg, gif or webp, or the send is refused), `wire`, `model` and
  `effort`; answers `turn`. `turn.steer` takes `session`, `expectedTurnId` and `text`; answers `turn` and `id`.
  `turn.sendNow` (capability `sendNow`) takes `id`, or none for every queued message; answers `ok`, errs
  `-32000` when no turn runs or the id is not queued. `turn.steered` follows each message the lead reads:
  `item` is its `id`, with `text`, `step` and `readAt`; a batch shares a `step`. `turn.steerQueued` tells the
  holder's clients a follower's steer is queued: `item` is its `id`, with `text`. `turn.unsteer` takes `text`,
  answers `removed`. `turn.stop` takes `turn`; it stops the lead and keeps running sub-agents, `everything: true` stops them too. `turn.send` during a turn queues a steer and answers `steered`, `expectedTurnId` refuses instead. `boardy.rows` answers the list's fields only. `undo` takes `session`,
  `turns`; answers `turns`, `restored`, `removed`, `refused` (`path`, `why`) and `dry_run`
- `mention.resolve`: takes `ref` and `session`; answers `outcome` (`item`, `not_found` or `ambiguous`), and an item's `item`, `speaker` and first `line`
- `shell.run`: takes `command`, runs it as `!` does, answers `output` and `stopped`. `shell.read` takes `shell` and `offset`; `shell.kill` `shell`
- take-over (capability `takeover`; once `initialize` declares it, every bash command runs in a terminal). `shell.takeover` takes `shell`, `cols` (2 to 1024) and `rows` (1 to 500); answers `shell`, `holder`
  (`person`), `since`, `cols`, `rows`, `base` and `next` (absolute byte offsets) and `replay`, the base64 raw bytes between them, colours kept; a second call answers a fresh replay.
  `shell.input` takes `shell` and `data`, base64 of 1 to 65536 bytes (Ctrl+C is `Aw==`); `shell.resize` takes `shell`, `cols`, `rows`; both answer `ok`. `shell.release` takes `shell` and `note` (at most 500 characters, quoted to the agent); answers
  `shell`, `holder` (`agent`) and `since`. `shell.holder` (`shell`, `holder`, `since`, `note`) tells every client of each change, an exit while held included, before `shell.exited`; `shell.raw` (`shell`, `offset`,
  `data`) streams the raw bytes while a person holds it, from `offset == next`. Bad params err `-32602`; a refusal errs `-32000` with `data.reason`: `not_declared`, `unknown_shell`, `not_running`, `not_kept`,
  `not_in_terminal`, `other_process` or `not_held` (input, resize or release on a shell nobody holds). `shell.started` and kept shells in `session.state` carry `terminal`, `holder` and `heldSince`
- reads, `query.` and: `models` (with `wires` signed in and `stale`), `settings` (each row's
  `label`, `description`, `kind` of `bool`, `int`, `text`, `choice` or `list`, `choices`, `min`, `max`
  with none for no ceiling, `unit`, `restart`), `commands` (each slash command's `name`, `args`,
  `summary`, `scope` `tofu` or `interface`, and `needs`, what a client must offer to show it),
  `rules` (each with `text`, `trigger`, `fires` and `fires_week`, seven days, today last), `agents`, `library`,
  `memory`, `hooks`, `changelog` (with `seen`), `update` (it never installs), `usage` (each provider row's
  `account`, `name`, `read_at`, `source`, `stale` and `retry_at`; the answer's `read_at` and `age_ms` are its
  oldest reading's), `doctor`, `accounts`, `cron`, `docs` (takes `topic`), `context` (takes `session`, else the
  open one, which before its first turn answers `unmeasured`; `items`, each `{band, kind, name, tokens,
  fate, step}`), `drain` (takes `session`; answers what `tofu session drain --json` prints) and `ledger` (takes `id`, or `last`, `point` and `session`, which keeps that session's rows before
  `last` counts; `rows` with `precedents` and `subject`, `{tool, command, path, url}`)
- boards (capability `boards`): `query.session` takes `session`; answers `info` and `generations`, the fork
  tree, each with `forked_into`, `fork_kind`, `tokens_before` and `tokens_after`. `query.usage.history` takes
  `range` (`day` by hour, `week` or `month` by day) and `project`; answers `buckets`, `total`, `sifted_tokens`
  and `spenders` by session, role, agent, wire, spend, account and model. `query.limits` answers `burns` per
  account and window since its last reset (`per_hour` null on one reading, `full_at` null when the window
  resets first), `order`, each role's accounts in turn, and `spend`, today's by account and agent.
  `query.skills` answers the library's `skills` and the folders'. `query.ledger.summary` takes `since`, a week
  back by default; answers `points`, each `count`, `week`, `would_ask`, `labeled`, `agreed`, `mean_ms`,
  `cost_usd` and `thresholds`
- memory: `memory.add` takes `text`, `scope` (`user-local`, `project-local`, `project-global`, `user-global`),
  `kind`; `memory.edit` `scope`, `id`, `text`; `memory.remove` `scope`, `id`; each answers the entry, which the lead hears.
  `memory.view` takes `scope` or `episodes`, `memory.zoom` `store`, `id`, `n`, `memory.recall` `store`, `regex`; each answers `store`, `lines` `{id, n, text}`
- `rules.add` takes `id`, `text`, `reason`, `concern`, `global`, `replace`; `rules.off` `id`, `reason`,
  `global`; `rules.remove`, `rules.restore` `id`, `global`. `agents.add` takes `name`, `description`, `model`,
  `tools`, `global`; `agents.set` `name`, `model`, `global`; `agents.remove` `name`, `global`. Each, and
  `learn.apply`, answers `changes` `{change, what, file}` (`added`, `changed`, `removed`) and `undo`
- `learn.scan` answers the run; `learn.show` takes `id`, `learn.reject` `id`, `reason`, each the finding; `learn.apply` takes `id`, `project`
- `reload` answers `project`, `first`, `parts` (`added`, `removed`, `changed`); `models.reload` `sources`, `versions`; `hooks.trust` `trusted`
- `setup.check` answers `steps`, each `{step, what, fix, done, choices}`; one with no `done` blocks a turn. `login.key` takes `provider`, `key` (checked),
  `login.logout` `role`, `provider`, `number`; both answer `note`. `login.start` takes `role` (`llm`,
  `classifier`, `search`), `provider`; answers the account signed in, `id`, `source`, `account`, `change`. `settings.set` takes `key`,
  `value`, `scope` (`global` or `project`); answers `key`, `value`, `scope`, and `source`, where the
  value now resolves from, and `settings.changed` tells every client
- `cron.command` takes the line you would type (`/cron delete all`), answers `note`. `label` takes `row`
  and `outcome`; answers `id`, `outcome`, `kind`, `verdict`. `scratch.list` (`scratch`) answers `root`, `folders`;
  `scratch.clean` takes `session`, `cache`, `dryRun`; answers `root`, `dry_run`, `removed`, the folders
  `tofu scratch clean --json` prints. `boardy.*` (`boardy`): `tofu docs boardy-views`. `boardy.questions {board?}` open questions from the ticket files, every board when board is left out; never rewrites catchup.md. `boardy.answer {ticket, id, answer}` one answer, as `tofu boardy catchup --answers`. `boardy.plan {board}` epics, milestones, sprints, view names. `boardy.init {key, name}` makes a board, as `tofu boardy init`. Every boardy answer carries `words`.
- `status.list` (`status`) takes `session`, none for every session open here, and errs on one not open;
  answers `records`: `session`, `id`, `state`, `kind`, `progress`, `msg`, `ask` (the approval or question a
  blocked record waits on) and `at`, when it entered its state. `status.ack` takes a `done` or `error`
  record's `id` and its `session`, else the one opened last, and clears it until it changes state

## Change it

A `remember` ask (`tofu/requestApproval`, `scope` is Jev's pick) answers `remember_global`,
`remember_user_local`, `remember_project` or `remember_project_local` as offered; `reject_*` keeps nothing, others err.

## Check it

`tofu serve --schema` prints `x-requests`, each method's `params` and `result`.

## Undo it

Every write answers `undo`; `memory.remove` takes back a `memory.add`.
