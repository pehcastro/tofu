---
title: Environment
description: env says whether a project can build on this machine and which keys are missing, and shell ports says who holds a port, without printing a value or killing a process.
order: 8
updated: 2026-11-06
---

`env` reads the toolchains a project declares and the keys its
`.env.example` names, and says for each what this machine has. It is tofu's
own Go code, takes no parameters and never prints the value of a key.

## Why it exists

**The missing piece is named before the first build fails.** Without it the
model learns that Go is too old or a key is unset from the error of a build
or a test, often after a long run, and has to read that error back to the
cause. `env` answers the question first, in a few lines: which toolchain is
wanted, which one is on `PATH`, and which key is not set.

**A key's value never reaches the model.** `env` says only whether `.env` or
the environment sets a key, so the answer is safe to keep in a session.

## What it reads

`env` looks for the toolchains in `go.mod`, `package.json` `engines.node`,
`rust-toolchain.toml` or `Cargo.toml` `rust-version`, `pyproject.toml`
`requires-python` and `requirements*.txt`. For each it states the version
wanted, the file that asks for it and the version on `PATH`, or `missing`
with the binaries it looked for, or `unknown` when the binary is there and its
version can't be read.

Then it reads the keys `.env.example` names and says, for each, whether
`.env` or the environment sets it. The first line is the verdict: `ready`, or
`not ready` with what is missing.

In a Go module whose `.env.example` names two keys and with no `.env`:

```bash
tofu tool env '{}' --dir .
```

```text
not ready: 2 of 2 env keys not set
toolchains:
go: want 1.22 from go.mod, have 1.25.7
env keys from .env.example:
ORDER_API_URL: missing, there is no .env
ORDER_DB_PATH: missing, there is no .env
```

With a `.env` that sets one of the two:

```text
not ready: 1 of 2 env keys not set
toolchains:
go: want 1.22 from go.mod, have 1.25.7
env keys from .env.example:
ORDER_API_URL: set in .env
ORDER_DB_PATH: missing from .env
```

A project with nothing missing and no `.env.example`:

```text
ready: every declared toolchain found and every env key set
toolchains:
go: want 1.25.0 from go.mod, have 1.25.7
env keys: no .env.example
```

## Who holds a port

`shell` with `op` `ports` says who listens on a port, one JSON line per port:
the `pid`, the `command`, the kept shell holding it, and `started_by` as
`this_session`, `other_session` or `outside`. Give `port` to ask about one
port, which answers `free` when nothing listens; leave it out to list every
port. It never kills a process, so a port held by something you started stays
yours. See [Shell](/docs/tools/shell).

`ports` reads the shells of a running session, so `tofu tool` can't run it:
outside a turn it answers `shell: no shell registry is attached to this turn`.

## Commands

You don't call `env`; the model does, before a build. `tofu tool env '{}'
--dir .` prints what it would read, and `tofu tool list --dir .` names `env`
among the tools a run offers.
