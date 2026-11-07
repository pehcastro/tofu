---
title: Tools overview
description: What a tool is in tofu, which ones are tofu's own Go code and which wrap a program, and what tofu does to every result.
order: 1
updated: 2026-11-06
---

A tool is a function the model can call during a turn. The model only asks:
tofu runs every call itself, in the working directory, and records it in the
session. The command line and the app build the same tool set.

Most tools are tofu's own Go code: `read`, `write`, `edit`, `glob`,
`search`, `symbols`, `map`, `api`, `project_report`, `fetch`, `web_search`, `plan`, `env`,
`http`, `record`, `shape`, `db`, `context`, `budget`, `forget`, `note`,
`digest`, `done`, `claims`, the browser tools and the sub-agent tools.
`profile` runs the project's own profiler. A few wrap a program: `bash` wraps your
shell, `typecheck` and `test` wrap the project's own checkers and test
runners for Go, Rust, Python and TypeScript, `github_pr_diff` wraps `gh`, and
the `tofu_*` verbs run the tofu binary.

Every tool runs by hand with `tofu tool`, which prints the exact text the
model would receive. See [One tool call by hand](/docs/cli/tool).

| Group | Tools |
|---|---|
| [Files](/docs/tools/files) | read, write, edit, glob, search, symbols, project_report |
| [Code intelligence](/docs/tools/code) | symbols, map, api |
| [Shell](/docs/tools/shell) | bash, shell |
| [Checks](/docs/tools/checks) | typecheck, test |
| [Web](/docs/tools/web) | web_search, fetch, github_pr_diff |
| [Work](/docs/tools/work) | plan, settings, artifact_fetch, history, ask_person, heads_up, skill, tofu verbs |
| [Sub-agents](/docs/tools/sub-agents) | spawn, race, message, subagents, ask |
| [Environment](/docs/tools/environment) | env, shell ports |
| [Observe](/docs/tools/observe) | http, record, profile |
| [Data](/docs/tools/data) | shape, db |
| [The agent's own context](/docs/tools/own-context) | context, budget, forget, note, digest |
| [Checking its own work](/docs/tools/check-work) | done, claims, the review agent |
| [Browser](/docs/automation/browser-tools) | browser_tabs, browser_observe, browser_act, browser_read, browser_do, browser_motion |

Each tool's description says only what the model needs before it calls the
tool. The guidance for a case the model rarely meets, such as an `edit` on a
file that does not exist, arrives in the error when that case happens, so every
request is smaller and the model still gets the help at the moment it needs
it. See [Context](/docs/features/context).

## What tofu does to every result

What the model reads is what it pays for, so tofu shapes every result before
the model sees it, in this order:

1. **Keys are redacted.**
2. **Shell output is sifted**, on `bash` only, when a classifier is set up.
3. **A large result is kept whole.** Over 32 KB, it's stored and the model
   gets a handle plus the first and last part, to read further with
   `artifact_fetch`.

A read-only call repeated with the same arguments in one turn is answered
from memory, and any other call clears that memory. A repeated `read` of a
file that hasn't changed answers with one line naming the earlier read,
whatever ran between. The same call returning
the same result three times in six calls ends the turn. Walking tools skip
`node_modules`, `.git`, `.tofu` and what `.gitignore` excludes.

## Changing what the model gets

You don't call tools. You change what the model gets:

- `readBeforeEdit` (on) refuses `write` and `edit` on a file the session
  hasn't read.
- `browser` (`drive`, `read` or `off`) and `browserDriver` decide the browser
  tools.
- An agent file's `tools:` line limits a sub-agent to the tools it names.

`tofu agents` prints each sub-agent's tools on a line of its own:

```text
    tools      read, search, glob, write, edit, bash
```
