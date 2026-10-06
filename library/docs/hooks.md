---
topic: hooks
title: Hooks
summary: the Claude Code and codex hooks tofu runs around a tool call, a prompt and a turn's end, and how a project's hooks are trusted
verbs: hooks
---

## What it is

A hook is a command you configured to run at a moment in a turn: before a
tool call, after one, when you send a prompt, or when the model is about to
stop. tofu runs the hooks you already wrote for Claude Code and codex,
unchanged. A hook reads one JSON document on standard input and answers
with its exit code and, optionally, JSON on standard output.

tofu fires these events, from the turn itself, so the app, `tofu run` and
every sub-agent run them alike:

- `PreToolUse`: before a tool call. Exit 2 refuses the call, and the model
  sees the hook's standard error as the reason. `permissionDecision` deny
  refuses too, ask asks you, and `updatedInput` rewrites the call.
- `PostToolUse`: after a tool call. Exit 2, or `decision: block`, puts the
  reason in front of the model beside the result.
- `UserPromptSubmit`: when a prompt starts a turn. Exit 2 refuses the
  prompt. Plain output becomes context for the model.
- `Stop`: when the model answers with no tool call. Exit 2 keeps the turn
  going with the reason as the next message, at most 8 times in a row.
- `SubagentStop`: the same, for one of tofu's sub-agents.
- `SessionStart`: at the first turn of a session, with source `startup`,
  `resume` after you continue a session, or `clear` after a fresh one.
  Its output becomes context for the model.
- `SessionEnd`: when the app exits, with reason `prompt_input_exit`, or when
  `tofu run` finishes, with reason `other`. It has 1.5 seconds.

A hook sees Claude's names: `Bash`, `Edit`, `Write`, `Read`, `Agent`, and
`file_path` rather than tofu's `path`. A matcher of `Edit` matches tofu's
`edit`, and an `updatedInput` written with `file_path` reaches tofu's tool
as `path`.

## Where it lives

- `~/.claude/settings.json`, `~/.codex/hooks.json` and `~/.tofu/hooks.json`:
  yours, trusted as they are.
- `.claude/settings.json`, `.claude/settings.local.json`,
  `.codex/hooks.json` and `.tofu/hooks.json` in a project: the project's,
  which run only once you trust them.

Every file has the same shape:

    {"hooks": {"PreToolUse": [{"matcher": "Bash",
      "hooks": [{"type": "command", "command": "./check.sh", "timeout": 10}]}]}}

tofu remembers what you trusted in `~/.tofu/hooks/trusted.json`, and each
hook's last result in `~/.tofu/hooks/last.json`.

## Trust

A project's hooks run commands from a repository you may have just cloned,
so tofu asks before it runs one. The app asks once, when a turn starts:
allow once runs them in that turn, always trusts them, deny refuses them.
`tofu run` cannot ask, so an untrusted hook does not run and the notice
says so.

Trust is a hash of the hook's entry and of every file its command names
inside the project, such as `.claude/hooks/check.ps1`. Change either and
the hook reads `changed since trusted` and is asked about again.

## Limits

- A hook has its own `timeout` in seconds, and 60 when it gives none, at
  most 600. On the timeout every process it started is killed, and the
  call goes on as if the hook had not answered.
- At most 4 hook processes run at once on the machine, across every tofu
  and every sub-agent. A hook waits for a slot inside its own timeout.
- A hook runs under bash unless it says `"shell": "powershell"`. On a
  machine with no bash, a bash hook is not run, and `tofu hooks` says
  why.
- A handler that appears in two settings files runs once.
- `rtk hook claude` is skipped: tofu already runs that rewrite.
- `http`, `prompt`, `agent` and `mcp_tool` hooks, `async` hooks and the
  `if` field are not run yet, and `tofu hooks` says so on each one.
- `disableAllHooks: true` in any of the files turns every hook off.
- tofu's keys are removed from a hook's environment, and
  `CLAUDE_PROJECT_DIR` is set to the project.

## Change it

    tofu hooks trust

trusts every hook in this project that is not trusted yet, as they are
now. Edit the settings file to change or remove a hook.

## Check it

    tofu hooks

lists every hook with its event, matcher, trust and command, then the file
it came from, its level, and its last result or the reason it is skipped.
`--json` prints one JSON document.

## Undo it

Remove a hook's line from `~/.tofu/hooks/trusted.json` to be asked about it
again, or set `disableAllHooks` to `true` in the project's
`.claude/settings.local.json` to turn every hook off.
