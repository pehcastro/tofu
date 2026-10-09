---
title: Scratchpad
description: Agents put probes, temporary files and build caches in a folder outside your repository, one folder per agent, cleaned on a schedule you set.
order: 20
updated: 2026-10-09
---

Agents need somewhere to write a probe script, a log, a capture or a build
target. tofu gives every project a scratchpad outside the repository, so
nothing an agent writes for itself lands in your tree or in a random folder
on your machine.

```
~/.tofu/projects/<project>/scratchpad/
  shared/                              notes the lead keeps; every agent reads them
  cache/                               cargo, pycache, pytest, ruff and mypy caches
  sessions/<session>/lead/             the lead's tmp, out and logs
  sessions/<session>/agents/<agent>/   one sub-agent's tmp, out and logs
```

## How it works

- Every command an agent runs gets its own `tmp/` as `TMPDIR`, and as `TMP`
  and `TEMP` on Windows, so every program that asks the system for a temp
  folder gets the agent's own.
- Build caches move out of the repository through the variables each tool
  already reads: `CARGO_TARGET_DIR`, `PYTHONPYCACHEPREFIX`, `RUFF_CACHE_DIR`,
  `MYPY_CACHE_DIR`, `GOTMPDIR`, and a `cache_dir` added to `PYTEST_ADDOPTS`.
- A sub-agent writes only its own folder. Two sub-agents that both write
  `tmp/x.txt` write two files, and a write into the other's folder or into
  the system temp folder is refused.
- The session folder is named by the readable session handle, so a socket
  path under `tmp/` stays short enough for macOS.

## Tools

- `scratch_path` returns the folder to use for tmp, out, logs, shared or cache.
- `scratch_list` lists an agent's own folder, the session's, or `shared/`.
- `scratch_read` reads another agent's output in the same session, or a
  file in another project's `shared/`. Reading another project asks you
  once per project; the lead raises the ask, never a sub-agent.
- `scratch_share` lets the lead copy a file into `shared/` with a note.

## Commands

| Command | What it does |
| --- | --- |
| `tofu scratch` | the root, each folder's size, and leftovers |
| `tofu scratch clean --dry-run` | what clean would remove |
| `tofu scratch clean` | remove it, stopping any process still running inside first |
| `tofu scratch clean --session X` | remove one session's folder |
| `tofu scratch clean --cache` | remove the build caches too |
| `tofu scratch open` | print the root |

## Cleanup

`scratchCleanupDays` (7 by default) removes the folder of a session no tofu
holds and nobody touched for that many days, when a run starts.
`scratchMaxGB` (50 by default) removes the oldest session folders first once
a project's scratchpad passes that size. `shared/` is cleaned only by hand.
`.tofu/scratch` folders left in a repository by older versions are listed by
`tofu scratch` and removed only by `tofu scratch clean`.

`scratchRoots` in `~/.tofu/settings.json` moves the scratchpad to a bigger
disk, as `<root>/tofu/<project>/scratchpad/`. A project's own settings
cannot set it, so a cloned repository never sends writes elsewhere.
