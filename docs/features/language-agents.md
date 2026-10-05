---
title: Language agents and the gate
description: tofu brings a sub-agent per language, and a gate that sends it back until its project's own checks ran after its last edit and passed.
order: 3
updated: 2026-10-05
---

A language agent is a built-in sub-agent for one language: `go-dev`,
`py-dev`, `rust-dev` and `ts-dev`. Each carries the rules and references for
its language and declares a **gate**, the checks that must run, and pass,
after its last edit of a file in that language.

![go-dev's report, after go vet and go test ran](./media/tui-task-answer.png)

| Agent | Gate |
|---|---|
| `go-dev` | `go vet`, `go test` |
| `py-dev` | `ruff check`, `pytest` |
| `rust-dev` | `cargo clippy`, `cargo test` |
| `ts-dev` | the `typecheck` and `test` tools |

When a language agent reports done, tofu walks its recorded calls. For each
gate check it looks for a run after the last edit in that language. A check
that didn't run, or exited non-zero, sends the agent back with what is
missing, up to 3 times; after that its claim stands with a warning.

A check counts however it was run: through `bash`, through the `typecheck`
or `test` tool, through `rtk`, or through the project's own `typecheck` or
`test` script run through `bash`. A typecheck that fails only on files the
sub-agent doesn't own doesn't send it back; it is judged on its own files.
When the project declares `vue-tsc` or `svelte-check`, the `typecheck` tool
runs that checker.

## Why a gate, and why it runs warm

With Claude Code or Codex, a Go or Rust specialist is a prompt you write and
keep up yourself. tofu brings one per language.

**The library changes what they write.** Against the same build without
their library, they caught a Rust stack overflow on deep input in 3 of 3 runs
against 1 of 6, cut Rust restriction lint findings from 6 to 2, Python ruff and
mypy findings from 3 to 0, and Go lint findings from 4 to 2.

**The gate checks the record, not the claim.** A report that says "tests
pass" proves nothing; a `go test` call after the last edit, exit 0, does.
The project's own recipes count, so `make check` passes for `go vet` when the
Makefile runs it, and so do `package.json` and `pyproject.toml` scripts.

**The checks run warm,** so the gate costs seconds. The TypeScript checker and
test runner stay up for the session: a 33-file rename in a large repository
went from 675 s to 94 s, and a test call takes 449 ms against 5 to 16 s for a
cold `npx vitest run`.

## Choosing and changing an agent

- **Use one**: you don't pick; the lead names the agent in its `spawn` call.
- **Change its model**: `tofu agents set go-dev claude-sub/claude-sonnet-5-5`.
- **Turn one off**: add `go-dev: none` to `.tofu/agent-models.yaml`.
- **Add a language**: write your own agent with `language` and `gate`. See
  [Agents](../library/agents).

## Commands

```sh
tofu agents
```

```text
  ● ts-dev    claude-sub/claude-sonnet-5-5     library  Writes and fixes
    TypeScript in the project it is given. Reads before it edits, changes the
    least it can, and is done only when the project's own typecheck, lint and
    tests pass.
    tools      read, glob, search, symbols, edit, typecheck, test, write, bash
    from       agent-models.yaml
    reference  ts-strict-config  2.8 KB
```
