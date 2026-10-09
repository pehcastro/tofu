---
topic: scratchpad
title: Scratchpad
summary: where agents put temporary files, probes and build caches, outside the repository, and how it is cleaned
verbs: scratch
---

## What it is

Every project has a scratchpad outside the repository. Agents write
probes, logs, captures and temporary files there, never in your repository
and never in the system temp folder.

Every command an agent runs gets its own `tmp/` as `TMPDIR`, and as `TMP`
and `TEMP` on Windows. Build caches move with `CARGO_TARGET_DIR`,
`PYTHONPYCACHEPREFIX`, `RUFF_CACHE_DIR`, `MYPY_CACHE_DIR`, `GOTMPDIR` and a
`cache_dir` added to `PYTEST_ADDOPTS`, so one sub-agent's check never
touches another's files.

Only the agent that owns a folder writes in it. Two sub-agents that both
write `tmp/x.txt` write two files, and a sub-agent's write into another's
folder or into the system temp folder is refused.

The agents have four tools for it:

- `scratch_path`: the folder to use for tmp, out, logs, shared or cache.
- `scratch_list`: the files in its own folder, the session's, or shared.
- `scratch_read`: a file another agent of the session wrote, or a file in
  another project's `shared/`. Reading another project asks you once per
  project, and the lead raises that ask, never a sub-agent.
- `scratch_share`: the lead copies a file into `shared/` with one line
  saying what it is.

## Where it lives

```
~/.tofu/projects/<project>/scratchpad/
  shared/                              notes the lead keeps; every agent reads them
  cache/                               build caches, one per project
  sessions/<session>/lead/             the lead's tmp, out and logs
  sessions/<session>/agents/<agent>/   one sub-agent's tmp, out and logs
```

`<session>` is the readable session handle, so a socket path under `tmp/`
stays short enough for macOS.

`scratchRoots` in `~/.tofu/settings.json` moves the scratchpad to another
disk, as `<root>/tofu/<project>/scratchpad/`. It is read from your home
settings only, never from a project, so a cloned repository cannot send
writes elsewhere.

## Change it

A session's folder that no running tofu holds and that nobody touched for
`scratchCleanupDays` days is removed when a run starts. Past `scratchMaxGB`
gigabytes the oldest session folders go first. A process still running in
a folder is stopped before it is removed. `shared/` is never cleaned on its
own.

```
tofu settings set scratchCleanupDays 3
tofu settings set scratchMaxGB 20
```

To clean by hand:

```
tofu scratch clean                 remove what cleanup would, and leftovers
tofu scratch clean --session X     remove one session's folder
tofu scratch clean --cache         remove the build caches too
```

## Check it

```
tofu scratch                       the root, each folder's size, and leftovers
tofu scratch clean --dry-run       what clean would remove
tofu scratch open                  print the root
```

`tofu scratch` also lists `.tofu/scratch` folders an older tofu left inside
the repository. They are removed only by `tofu scratch clean`.

## Undo it

A removed folder is gone. Run `tofu scratch clean --dry-run` first to see
the list, and keep anything you want in `shared/`, which no cleanup
touches. The defaults come back with:

```
tofu settings set scratchCleanupDays 7
tofu settings set scratchMaxGB 50
```
