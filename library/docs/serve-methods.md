---
topic: serve-methods
title: Every method tofu serve answers
summary: the requests of tofu.host/1, what each takes and what it answers, for a frontend wiring tofu serve
verbs: serve
---

## What it is

Every request `tofu serve --stdio` answers, with what it takes and what it
answers. `tofu docs serve` says what serve is and how to start it, and
`tofu serve --schema` carries every field of every line. Reads, reloads,
`session.list`, `session.history`, `session.info`, `session.find`,
`session.trace`, `mention.resolve`, `shell.run`, `learn.scan`, `setup.check`
and the logins answer out of order, so none of them holds `turn.stop`.

A read, a reload, a write and `learn.*` answer the report the verb's `--json`
prints under `data`, with no envelope; a verb that fails errs with its
problems. Only `undo`, `label`, `settings.set` and `login.start` answer the
envelope, `{tofu, verb, ok, at, data, problems}`.

## Where it lives

- `initialize`: takes `client`, `versions` and `capabilities`, where
  `questions` says it answers `tofu/askPerson`; answers `protocol`, `tofu`,
  `project` and `capabilities`, one name a family of methods
- `session.list`: takes `search` and `limit`; answers `head` and `sessions`,
  a row each with `id`, `name`, `handle`, `task`, `turns`, `lastAt`, `wire`,
  `model`, `costUsd`, `outcome` as the session recorded it, `open` for the
  one open here, `running` while a turn runs, and `heldBy` for another tofu's
- `session.open`: takes `session` (none starts a fresh one), `asking` and
  `replay`, how many of the newest lines it sends; answers `session` and
  `fresh`. A session another tofu holds errs `session.busy`, naming the process
- `session.state`: `running` and its `turn`, `asking`, the `pick` of wire,
  model and effort, waiting approvals, open `questions`, sub-agents, the kept
  shells with the fields `shell.started` carries, the context window and the
  cron jobs; it is the answer to `resync`
- `session.set`: takes `asking`, `wire`, `model` and `effort`, held for later
  turns and cron fires; `session.settings` tells every client
- `session.rename`: takes `session` and `name`, and names the whole family
- `session.compact`: answers `results`, `tokensBefore`, `tokensAfter`, and
  `into` when anything shrank
- `session.history`: takes `session`, `limit` and `before`; answers `lines`,
  each `{method, params}` as `session.open` sends it, from `first` of `total`
- `session.info`: takes `session`; answers the row `tofu session info` prints
- `session.find`: takes `session` and any of `tool`, `command`, `file`,
  `text`, `agent`, `since` and `until`, each a duration like `2h` or a time;
  answers `handle`, `query` and `hits`
- `session.trace`: takes `session`; answers what `tofu session trace` prints:
  `requests`, `calls`, `hooks`, `failures`, `messages`, `agents` and `cache`
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
- reads, `query.` and: `models` (the models, `wires` signed in and `stale`),
  `settings`, `rules`, `agents`, `library`, `memory`, `hooks`, `changelog`
  (every version, and `seen`, the newest you read), `update` (it never
  installs), `usage` (each provider row names its `account`, the id
  `quota.updated` uses, and its `name`; the last reading, at once, with `read_at` and
  `age_ms`; the first is read when a session opens, and one past five
  minutes is read again behind the answer), `doctor`, `accounts` (`subscriptions` with their
  `accounts`, and `keys`), `cron`, `docs` (takes `topic`; answers `topics` and
  `entries`, or `page`), `context` (takes `session`) and `ledger` (takes `id`,
  or `last` and `point`; answers `rows`, each with its `precedents`)
- memory: `memory.add` takes `text`, `scope` (`global` or `project`) and
  `kind`; `memory.edit` takes `scope`, `id` and `text`; `memory.remove` takes
  `scope` and `id`. Each answers the entry, `{id, scope, kind, text, said, at,
  by, file}`, and the running lead hears an add or an edit
- rules: `rules.add` takes `id`, `text`, `reason`, `concern`, `global` and
  `replace`; `rules.off` takes `id`, `reason` and `global`; `rules.remove` and
  `rules.restore` take `id` and `global`
- agents: `agents.add` takes `name`, `description`, `model`, `tools` and
  `global`; `agents.set` takes `name`, `model` and `global`; `agents.remove`
  takes `name` and `global`
- every rules and agents write, and `learn.apply`, answers `changes`, each
  `{change, what, file}` with `change` `added`, `changed` or `removed`, and
  `undo`, the command that takes it back
- learn: `learn.scan` answers the run; `learn.show` takes `id` and
  `learn.reject` `id` and `reason`, each answering the finding;
  `learn.apply` takes `id` and `project`
- `reload` answers `project`, `first` and `parts`, each with `added`,
  `removed` and `changed`; `models.reload` answers `sources` and `versions`
- `hooks.trust` trusts every new project hook and answers `trusted`
- `setup.check` answers `steps`, each `{step, what, fix, done, choices}`; one
  with no `done` still blocks a turn. `login.key` takes `provider` and `key`
  and checks the key with the provider; `login.logout` takes `role`,
  `provider` and `number`; both answer `note`. `login.start` takes `role` and
  `provider`. `settings.set` takes `key`, `value` and `scope`
- `cron.command` takes the line you would type, such as `/loop 10m check the
  build`, or `/cron delete all`, and answers `note`. `label` takes `row` and `outcome`
- `status.list` (capability `status`): takes nothing; answers `records`, every
  `status` record as it stands, each `id`, `state`, `kind`, `progress`, `msg`

## Change it

A `remember` ask (`tofu/requestApproval`, tool `remember`, `scope` is Jev's
pick) answers `remember_global` (user global), `remember_user_local`,
`remember_project` (project global) or `remember_project_local`; `reject_*`
keeps nothing, `allow_*` is ignored, and other asks ignore `remember_*`.

## Check it

    tofu serve --schema

prints `x-requests`, every method above with its `params` and `result`.

## Undo it

Every write answers `undo`, the command that takes it back; `memory.remove`
takes back a `memory.add`.
