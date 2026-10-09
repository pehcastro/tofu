---
topic: serve-methods
title: Every method tofu serve answers
summary: the requests of tofu.host/1, what each takes and what it answers, for a frontend wiring tofu serve
verbs: serve
---

## What it is

Every request `tofu serve --stdio` answers; `tofu docs serve` says how to
start it, `tofu serve --schema` carries every field. Reads, reloads, the
`session.*` and `memory.*` reads, `mention.resolve`, `shell.run`,
`learn.scan`, `setup.check` and the logins answer out of order, so none of
them holds `turn.stop`.

A read, a reload, a write and `learn.*` answer the verb's `--json` report with
no envelope; a verb that fails errs with its problems. Only `undo`, `label`,
`settings.set` and `login.start` answer `{tofu, verb, ok, at, data, problems}`.

## Where it lives

- `initialize`: takes `client`, `versions` and `capabilities`, where
  `questions` says it answers `tofu/askPerson`; answers `protocol`, `tofu`,
  `project` and `capabilities`, one name a family of methods
- `session.list`: takes `search`, `limit` and `kind`; answers `head` and
  `sessions`, each `id`, `name`, `handle`, `task`, `turns`, `lastAt`, `wire`,
  `model`, `costUsd`, `outcome`, `open` (here), `running`, `heldBy` (another
  tofu) and `kind`, `main` or `side`; a side chat adds `parent`, `owns`, `preset`
- side chats (capability `side`): `session.branch` takes `session`, `kind:
  "side"`, `seed` (`summary` or `none`), `owns` or `preset`, and `name`;
  answers `session`, `handle`, `parent` `{session, event}`, `owns`, `preset`
  and `carried`. `session.access` takes `session` and `owns` or `preset`,
  `read` with neither, and errs while that chat's turn runs
- `session.open`: takes `session` (none starts a fresh one), `asking` and
  `replay`, how many of the newest lines it sends; answers `session` and
  `fresh`. A session another tofu holds errs `session.busy`, naming the process
- `session.state`, the answer to `resync`: `running` and its `turn`, `asking`,
  the `pick`, waiting approvals, `standing` answers (`target`, `decision`;
  kept beside the session, read back when it reopens, carried into its
  compaction and never into another session), open `questions`, sub-agents, kept shells, the context and the cron jobs
- `session.set`: takes `asking`, `wire`, `model` and `effort`, held for later
  turns and cron fires; `session.settings` tells every client
- `session.rename`: takes `session` and `name`, and names the whole family
- `session.compact`: answers `results`, `tokensBefore`, `tokensAfter`, `into`.
  `session.history`: takes `session`, `limit` and `before`; answers `lines`,
  each `{method, params}` as `session.open` sends it, from `first` of `total`
- `session.info` and `session.trace` answer what `tofu session info` and
  `trace` print. `session.find` takes `session` and any of `tool`, `command`,
  `file`, `text`, `agent`, `since`, `until`; answers `handle`, `query`, `hits`
- `turn.send`: takes `session`, `text`, `mentions` (each a `ref`, kept where
  `text` has it, else added at the end; a path gains `@`), `images` (png, jpg,
  gif or webp, or the send is refused), `wire`, `model` and `effort`; answers
  `turn`. `turn.steer` takes `session`, `expectedTurnId` and `text`; answers
  `turn` and `id`. `turn.sendNow` (capability `sendNow`) takes `id`, or none
  for every queued message; answers `ok`, errs `-32000` when no turn runs or
  the id is not queued. `turn.steered` follows each message the lead reads:
  `item` is its `id`, with `text`, `step` and `readAt`; a batch shares a
  `step`. `turn.unsteer` takes `text`, answers `removed`. `turn.stop` takes
  `turn`, `lead: true` stops the lead alone. `undo` takes `session`, `turns`
- `mention.resolve`: takes `ref` and `session`; answers `outcome` (`item`,
  `not_found` or `ambiguous`), and an item's `item`, `speaker` and first `line`
- `shell.run`: takes `command`, runs it as `!` does, answers `output` and
  `stopped`. `shell.read` takes `shell` and `offset`; `shell.kill` `shell`
- reads, `query.` and: `models` (with `wires` signed in and `stale`),
  `settings`, `rules` (each with `text`, `trigger`, `fires` and `fires_week`,
  seven days, today last), `agents`, `library`, `memory`, `hooks`,
  `changelog` (with `seen`), `update` (it never installs), `usage` (each
  provider row's `account`, `name`, `read_at`, `source`, `stale` and
  `retry_at`; the answer's `read_at` and `age_ms` are its oldest reading's),
  `doctor`, `accounts`, `cron`, `docs` (takes `topic`), `context` (takes
  `session`; `items`, each `{band, kind, name, tokens, fate, step}`) and
  `ledger` (takes `id`, or `last` and `point`; `rows` with `precedents` and
  `subject`, `{tool, command, path, url}`)
- boards (capability `boards`): `query.session` takes `session`; answers
  `info` and `generations`, the fork tree, each with `forked_into`,
  `fork_kind`, `tokens_before` and `tokens_after`. `query.usage.history`
  takes `range` (`day` by hour, `week` or `month` by day) and `project`;
  answers `buckets`, `total`, `sifted_tokens` and `spenders` by session, role,
  agent, wire, spend, account and model. `query.limits` answers `burns` per
  account and window since its last reset (`per_hour` null on one reading,
  `full_at` null when the window resets first), `order`, each role's accounts
  in turn, and `spend`, today's by account and agent. `query.skills` answers
  the library's `skills` and the folders'. `query.ledger.summary` takes
  `since`, a week back by default; answers `points`, each `count`, `week`,
  `would_ask`, `labeled`, `agreed`, `mean_ms`, `cost_usd` and `thresholds`
- memory: `memory.add` takes `text`, `scope` (`user-local`, `project-local`,
  `project-global`, `user-global`), `kind`; `memory.edit` `scope`, `id`, `text`;
  `memory.remove` `scope`, `id`; each answers the entry, which the lead hears.
  `memory.view` takes `scope` or `episodes`, `memory.zoom` `store`, `id`, `n`,
  `memory.recall` `store`, `regex`; each answers `store`, `lines` `{id, n, text}`
- `rules.add` takes `id`, `text`, `reason`, `concern`, `global`, `replace`;
  `rules.off` `id`, `reason`, `global`; `rules.remove`, `rules.restore` `id`,
  `global`. `agents.add` takes `name`, `description`, `model`, `tools`,
  `global`; `agents.set` `name`, `model`, `global`; `agents.remove` `name`,
  `global`. Each, and `learn.apply`, answers `changes` `{change, what, file}`
  (`added`, `changed`, `removed`) and `undo`
- `learn.scan` answers the run; `learn.show` takes `id`, `learn.reject` `id`,
  `reason`, each the finding; `learn.apply` takes `id`, `project`
- `reload` answers `project`, `first`, `parts` (`added`, `removed`, `changed`);
  `models.reload` `sources`, `versions`; `hooks.trust` `trusted`
- `setup.check` answers `steps`, each `{step, what, fix, done, choices}`; one
  with no `done` blocks a turn. `login.key` takes `provider`, `key` (checked),
  `login.logout` `role`, `provider`, `number`; both answer `note`.
  `login.start` takes `role`, `provider`; `settings.set` `key`, `value`, `scope`
- `cron.command` takes the line you would type (`/cron delete all`), answers
  `note`. `label` takes `row` and `outcome`
- `status.list` (`status`) answers `records`: `id`, `state`, `kind`, `progress`, `msg`

## Change it

A `remember` ask (`tofu/requestApproval`, `scope` is Jev's pick) answers
`remember_global`, `remember_user_local`, `remember_project` or
`remember_project_local` as offered; `reject_*` keeps nothing, others err.

## Check it

`tofu serve --schema` prints `x-requests`, each method's `params` and `result`.

## Undo it

Every write answers `undo`; `memory.remove` takes back a `memory.add`.
