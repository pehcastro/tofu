---
topic: hooks
title: Hooks
summary: the Claude Code and codex hooks tofu runs around a tool call, a prompt and a turn's end, and how a project's hooks are trusted
verbs: hooks
---

## What it is

A hook is a command you configured to run at a moment in a turn. tofu runs
the hooks you already wrote for Claude Code and codex, unchanged. A hook
reads one JSON document on standard input and answers with its exit code
and, optionally, JSON on standard output. The events fire from the turn
itself, so the app, `tofu run` and every sub-agent run them alike:

- `PreToolUse`: before a tool call. Exit 2 or `permissionDecision` deny
  refuses it, ask asks you, and `updatedInput` rewrites it.
- `PostToolUse`: after a tool call. Exit 2 or `decision: block` puts the
  reason beside the result.
- `UserPromptSubmit`: when a prompt starts a turn. Exit 2 refuses it; plain
  output becomes context.
- `Stop` and `SubagentStop`: when the model or a sub-agent answers with no
  tool call. Exit 2 keeps it going, at most 8 times in a row.
- `SessionStart`: source `startup`, `resume`, `clear`, or `compact` after
  tofu compacts inside a turn. Its output becomes context.
- `SessionEnd`: when the app or `tofu drive` exits (`prompt_input_exit`) or
  `tofu run` finishes (`other`). It has 1.5 seconds.
- `GateVerdict`, tofu only: after Jev's gate decides on a call, with the
  verdict, the risk and every answer under `gate`. It matches the tool name.
  `permissionDecision` can turn an ask into allow or deny, a deny into ask,
  or tighten any verdict, never a deny into allow. Exit 2 is deny, the
  strictest of several hooks wins, and a hook-made ask goes to you. It does
  not fire on calls only you may answer, a failed gate, or a gate in shadow.
- `SubagentSpawn`, tofu only: before a sub-agent starts, with definition,
  mission, the task it is given and owns under `spawn`. It matches the
  definition's name. Exit 2
  or `decision: block` refuses the spawn; `owns` narrows the paths, `[]`
  takes them all. A glob not provably inside what was asked, or two hooks
  giving different owns, refuses the spawn.

A hook sees Claude's names: `Bash`, `Edit`, `Write`, `Agent`, and
`file_path` rather than `path`, both ways.

Each event accepts its own fields. `GateVerdict` takes `permissionDecision`
and `permissionDecisionReason`; `SubagentSpawn` takes `decision`, `reason`
and `owns`; every event takes `systemMessage` and `suppressOutput`, and
`hookSpecificOutput` needs a `hookEventName` naming its event. A misspelt
or foreign field rejects the whole reply, and the problem shows in the
trace and in `tofu hooks`.

## Where it lives

- `~/.claude/settings.json`, `~/.codex/hooks.json`, `~/.codex/config.toml`
  and `~/.tofu/hooks.json`: yours, trusted as they are.
- `.claude/settings.json`, `.claude/settings.local.json`,
  `.codex/hooks.json`, `.codex/config.toml` and `.tofu/hooks.json` in a
  project: run only once you trust them.

codex's `config.toml` keeps them under `[hooks]`. Every other file:

    {"hooks": {"SubagentSpawn": [{"matcher": "go-dev",
      "hooks": [{"type": "command", "command": "./narrow.sh", "timeout": 10}]}]}}

where `./narrow.sh` prints
`{"hookSpecificOutput":{"hookEventName":"SubagentSpawn","owns":["src/**"]}}`.

The app asks about a project's hooks once, when a turn starts: allow once,
always, or deny. `tofu run` cannot ask, so an untrusted hook does not run.
Trust is a hash of the entry and of every script it names in the project;
change either and it is asked about again. tofu keeps trust in
`~/.tofu/hooks/trusted.json` and last results in `~/.tofu/hooks/last.json`.

Limits:

- `timeout` is in seconds, 60 by default, at most 600. On a timeout every
  process the hook started is killed and the hook counts as no answer.
- At most 4 hook processes run at once across the machine.
- A hook runs under bash unless it says `"shell": "powershell"`.
- A handler in two files runs once; `rtk hook claude` is skipped.
- `http`, `prompt`, `agent`, `mcp_tool` and `async` hooks and the `if`
  field are not run yet, and `tofu hooks` says so.
- tofu's keys leave the environment; `CLAUDE_PROJECT_DIR` is set.

## Change it

    tofu hooks trust

trusts every hook in this project that is not trusted yet. Edit the
settings file to change or remove a hook.

## Check it

    tofu hooks

lists every hook with its event, matcher, trust, command, file, level, and
its last result or why it is skipped. `--json` prints one document.

    tofu session trace <session>

lists every hook run under `hooks`, in order, beside its call: event, exit
code, duration, decision, command, source, any problem, and stderr capped
at 200 bytes with keys masked. A call a `GateVerdict` hook changed says so
on its row under `calls`, with the verdict the hook gave.

## Undo it

Remove a hook's line from `~/.tofu/hooks/trusted.json` to be asked again,
or set `"disableAllHooks": true` in `.claude/settings.local.json` to turn
every hook off.
