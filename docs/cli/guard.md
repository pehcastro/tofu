---
title: Guard rules
description: tofu guard runs a repository's Claude Code and git hook rules inside the binary, so the same hooks answer the same way on Windows, Linux and macOS.
order: 3
updated: 2026-10-24
---

`tofu guard <rule>` answers a [Claude Code hook](https://docs.claude.com/en/docs/claude-code/hooks)
the way a hook script does: it reads the hook's JSON on standard input and exits 0 to allow, exits 2
with a reason on standard error to refuse, or prints a `permissionDecision` deny. `tofu guard git
commit-msg <file>` is a [git commit-msg hook](https://git-scm.com/docs/githooks#_commit_msg): it
exits 1 with the reason when the message breaks a rule.

## Why the rules live in the binary

**One hook runs everywhere.** A hook written in PowerShell runs on Windows only, and a shell script
needs a shell. The rules are Go inside tofu, so `.claude/settings.json` and `.githooks` name the same
command on every OS, and one process start answers each call.

**The answers did not change.** Every rule was checked against the script it replaces on a recorded
set of inputs, each allowed and refused case built from the script's own conditions: same exit code,
same first line.

## The rules

| Rule | Hook | Refuses |
| --- | --- | --- |
| `emdash` | PreToolUse Write, Edit | a write carrying an em dash |
| `comments` | PreToolUse Write, Edit | a comment in a Go file; compiler directives pass |
| `library-wording` | PreToolUse Write, Edit | a ticket id, a dated report or a person's decision under `library/` |
| `shell` | PreToolUse Bash, PowerShell | a shell write of source, a discard of uncommitted work, git from a sub-agent, a worktree, `go test` over a pattern without `-p`, a tree-wide check while a doing ticket owns Go |
| `loc-track` | PostToolUse Write, Edit | nothing; records each Go file written |
| `snapshot` | PostToolUse Write, Edit | nothing; copies each tracked file to the user cache folder |
| `minify-gate` | Stop, SubagentStop | the first stop before a minify pass over the files written |
| `require-ticket` | PreToolUse Agent, Task | a spawn without a live ticket that has Acceptance, owns, a matching owner and no overlap |
| `own-paths` | PreToolUse Write, Edit | a sub-agent write outside every doing ticket's owns |
| `ticket-log` | PreToolUse Bash, PowerShell | moving a ticket out of review with a Log under 200 characters |
| `research-used` | PreToolUse Write, Edit | a plan or ticket citing research that does not record the citation |

The ticket rules read the project's boards when `projectManagement` is `boardy` and a board exists,
and the project's ticket folders otherwise.

`require-ticket` lets a read-only agent start with no ticket while its spawn names no ticket and no
owns. An agent is read-only when the `tools:` of its definition hold no edit or patch tool, read from
the `agents` folders under `.tofu`, `.agents` and `.claude` and from the library. The refusal names
the read-only agents the project has, so you can spawn one to understand a problem before you write
its ticket, and the lead's spawn tool says which agents need a ticket.

## Use it

```json
{ "type": "command", "command": "tofu guard emdash" }
```

```sh
#!/bin/sh
exec tofu guard git commit-msg "$1"
```

## Check it

```sh
echo '{"tool_input":{"command":"git worktree add ../x"}}' | tofu guard shell
```

prints a deny whose reason opens `git worktree is refused in this repository.` A commit message with
a 73-character subject prints `commit refused: the subject is 73 characters, the limit is 72.` and
exits 1. An unknown rule exits 2 and lists the rules.
