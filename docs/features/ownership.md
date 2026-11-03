---
title: Ownership
description: Every sub-agent holds the paths it may write, tofu checks every write against them, and sub-agents with different paths run side by side.
order: 4
updated: 2026-10-13
---

Every sub-agent that can write holds a list of paths, its `owns`: a file, a
folder, or a glob such as `internal/agent/loop/*.go`. The lead gives it in the
`spawn` call. tofu checks every write against it before anything changes.

![The file edits tab: each changed file and the sub-agent that changed it](./media/tui-file-edits.png)

- `write` and `edit` outside `owns` fail.
- A `bash` command is read before it runs, step by step. Outside `owns` it
  runs only when every step reads: reading, listing, searching, inspecting,
  `git` reads, and the project's own checks such as `go test`, `cargo
  test` or `npm test`, and the formatters `gofmt` and `rustfmt`. A
  formatter that rewrites files runs only when the files are in `owns` or
  the scratch folder. `cargo build`, `cargo run` and a binary they built
  run for a crate the agent holds a path in, into the project's one shared
  target; `--target-dir` and a `CARGO_TARGET_DIR` of its own are refused. `rm`, `mv`, a redirect, `sed -i`, `bash -c`,
  PowerShell's `Set-Content` and anything the check can't read are refused
  with the reason. A write into the agent's own scratch folder is
  allowed, and `TMP`, `TEMP` and `TMPDIR` point there; the system's
  temporary folder is not.
  The same check runs on Git Bash, PowerShell, Linux and macOS.
- A source file, such as `.go` or `.ts`, changes only through `edit` or
  `write`, never the shell.
- A command over the whole tree, such as `go test ./...` or `gofmt -l .`,
  is judged by the folders it reaches. When the agent holds a file in every
  folder it reaches, as in a one-package project, it runs. Otherwise it is
  refused, and it is the lead's to run once, after the sub-agents finish.
  `go test all` is always refused.

## Inline code asks you

A sub-agent's inline code runs only when you allow it. Inline code is one
`python` or `node` call with `-c` or `-e`, a heredoc such as
`python - <<'EOF'`, or a script in the agent's scratch folder, with nothing
piped or chained.

1. The lead sees the code and may refuse it. It can't allow it; its allow
   passes the code on to you.
2. You see the exact code with hidden and bidirectional characters escaped,
   its sha256, the interpreter and flags, the folder it runs in, and a line
   saying network and file reads stay open, because no sandbox holds it.
3. You answer allow once, deny, always here or never here. Always here
   covers that exact code, from that agent, until tofu closes. Never here
   is kept, and the same code is refused again without asking anyone.

Allowed code runs from a copy of the bytes you saw, in the agent's scratch
folder, with `python -I` for Python. Its environment keeps only an allowlist,
and any name holding `KEY`, `TOKEN`, `SECRET`, `PASSWORD`, `CREDENTIAL` or
`AUTH` is left out. It's killed after 2 minutes and never moves to the
background. Each ask and each run writes an `inline_code` row to the decision
ledger, with the code, its sha256, who answered, the answer, the exit code and
the duration.

## Running side by side

Two running sub-agents never hold overlapping paths. A spawn that would is
refused and told to `message` the holder instead. After a restart, the
session's earlier sub-agents hold no paths, so a spawn on their files
starts at once, and one of them takes its paths back when a message
resumes it and nobody else holds them.

## Why paths are held

**Parallel work needs disjoint files.** Two agents editing one file overwrite
each other. Because paths can't overlap, spawns in one answer start together:
3 of 3 ran at once and the turn took 3 s, where the same shape ran 0 of 4 at
once before.

**The shell is not a way around it.** A list of paths means little if
`bash` can write anywhere. Across 79 commands that try to change the project
from outside a sub-agent's paths, on Git Bash for Windows, PowerShell 5.1
and Linux bash, none changed a file.

**Every change shows in the diff.** A source edit through the shell skips the
read check, the diagnostics and the **file edits** tab. Routing it through
`edit` keeps all three honest.

**A refusal is a question, not a loss.** The refused path goes into the
sub-agent's report, so the lead can widen the brief or hand the path to its
holder.

## Seeing who changed what

- **See who changed what**: the **file edits** tab, `Alt+3`.
- **Refuse edits to unread files**: `readBeforeEdit`, on by default, refuses
  an edit or write to a file this session hasn't read, or that changed since.
- You don't write `owns` yourself; the lead does, per spawn.

## Commands

```sh
tofu settings get readBeforeEdit
```

```text
true
```
