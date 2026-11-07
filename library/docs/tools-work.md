---
topic: tools-work
title: Work, web, environment and delegation tools
summary: what plan, history, fetch, web_search, env, http, record, profile, shape, db, spawn, race, ask_person and heads_up give the model
verbs: tool
---

## What it is

These tools organize the work, read the web and the machine, and reach
sub-agents and the person.

- A `plan` item may carry a `check`, a command or an acceptance line.
  `done` on that item needs `call`, the id of the earlier tool call in the
  session that ran the check, and is refused without one or with an id no
  earlier call has.
- `history` is one tool with a `mode`: `quote` for a `[quote#...]`
  reference you wrote, `zoom` and `recall` over the memory views, and
  `lookup` for a call a fork's carry names.
- `fetch` puts `date: published YYYY-MM-DD`, `modified ...`, or `date:
  none stated` under its header. `web_search` puts each result's date
  beside its title when the provider gives one.
- `env` says `ready` or `not ready`, each toolchain the project declares
  with the version wanted and the one on `PATH`, and each key
  `.env.example` names as set or missing. It never prints a value.
- `shell` with `op` `ports` gives one JSON line per port: `pid`,
  `command`, the kept shell and `started_by`, or `free`. It never kills.
- `http` calls a server on this machine only and returns the status, the
  time, the headers worth reading and the body. `record` with `start`
  saves the plain http a command makes, and `replay` answers the same
  command from the recording with the server down.
- `profile` runs `go test` with one package, `node` or `python` under a
  profiler and returns the ten costliest functions and the hottest path.
- `shape` prints the type of a JSON sample in the project's language.
  `db` reads SQLite or Postgres read only, with the connection from a key
  in `.env`, `DATABASE_URL` by default, never printed.
- `spawn` with `estimate` set to `true` returns the median USD, model
  calls and wall time of the last 20 finished runs of that agent type in
  this project, and starts nothing.
- `race` takes 2 to 3 `briefs`, one whole approach each, and a `check`.
  Each runs on its own sub-agent in its own copy of the project; the first
  whose check passes is applied and the rest are stopped. When none passes
  the tree is byte for byte as it was.
- `ask_person` options may carry a `preview`, as code, a diff or a text
  screen, shown side by side. `heads_up` shows one line on the person's
  screen at `info` or `warn`, and the turn goes on.

```text
not ready: 1 of 2 env keys not set
toolchains:
go: want 1.22 from go.mod, have 1.25.7
env keys from .env.example:
ORDER_API_URL: set in .env
ORDER_DB_PATH: missing from .env
```

## Where it lives

`race` copies the project under the scratchpad; see `tofu docs
scratchpad`. `db` reads its connection from the project's `.env`.

## Change it

    tofu settings set turnMaySpawn false
    tofu settings set subAgentsPerTurn 20

## Check it

    tofu tool env '{}' --dir .
    tofu tool db '{"env":"ORDER_DB_PATH"}' --dir .
    tofu tool shape '{"path":"orders.json"}' --dir .
    tofu tool spawn '{"estimate":true,"agent":"go-dev"}' --dir .
    tofu tool heads_up '{"level":"info","line":"the full test run takes about two minutes"}' --dir .

`tofu docs asking` says how the lead's questions reach you.

## Undo it

    tofu settings set turnMaySpawn true
    tofu settings set subAgentsPerTurn 10
