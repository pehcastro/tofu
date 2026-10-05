---
title: Tools overview
description: What a tool is in tofu, which ones are tofu's own Go code and which wrap a program, and what tofu does to every result.
order: 1
updated: 2026-10-04
---

A tool is a function the model can call during a turn. The model only asks:
tofu runs every call itself, in the working directory, and records it in the
session. The command line and the app build the same tool set.

Most tools are tofu's own Go code: `read`, `write`, `edit`, `glob`,
`search`, `symbols`, `project_report`, `fetch`, `web_search`, `plan`, the
browser tools and the sub-agent tools. A few wrap a program: `bash` wraps your
shell, `typecheck` wraps `tsc`, `test` wraps Vitest, `github_pr_diff` wraps
`gh`, and the `tofu_*` verbs run the tofu binary.

| Group | Tools |
|---|---|
| [Files](/docs/tools/files) | read, write, edit, glob, search, symbols, project_report |
| [Shell](/docs/tools/shell) | bash, shell |
| [Checks](/docs/tools/checks) | typecheck, test |
| [Web](/docs/tools/web) | web_search, fetch, github_pr_diff |
| [Work](/docs/tools/work) | plan, settings, artifact_fetch, quote, skill, tofu verbs |
| [Sub-agents](/docs/tools/sub-agents) | spawn, message, subagents, ask |
| [Browser](/docs/automation/browser-tools) | browser_tabs, browser_observe, browser_act, browser_read, browser_do, browser_motion |

## What tofu does to every result

What the model reads is what it pays for, so tofu shapes every result before
the model sees it, in this order:

1. **Keys are redacted.**
2. **Shell output is sifted**, on `bash` only, when a classifier is set up.
3. **A large result is kept whole.** Over 32 KB, it's stored and the model
   gets a handle plus the first and last part, to read further with
   `artifact_fetch`.

A read-only call repeated with the same arguments in one turn is answered
from memory, and any other call clears that memory. The same call returning
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
