---
title: Hooks
description: The Claude Code and codex hooks you already wrote run under tofu unchanged, around every tool call, prompt and turn's end, and a project's hooks run only once you trust them.
order: 13
updated: 2026-10-06
---

A hook is a command you configured to run at a moment in a turn. tofu reads
the hooks you already have in `.claude/settings.json`, `.codex/hooks.json`,
the `[hooks]` table of `.codex/config.toml` and `.tofu/hooks.json`, in your
home and in the project, and runs them with
Claude Code's rules: the same events, the same JSON on standard input, and
the same meaning for exit 0, exit 2 and the JSON a hook prints.

## Why hooks work the way they do

**Your hooks keep working.** A hook sees `Bash`, `Edit` and `Write` with
`file_path`, as Claude Code sends them, and a rewrite it returns in those
names reaches tofu's own tools. A matcher like `Write|Edit` matches.

**Every turn runs them, sub-agents too.** The hooks fire from the turn
itself, so the app, `tofu run` and every sub-agent run the same hooks.
`SubagentStop` fires when one of tofu's sub-agents finishes.

**A cloned repository cannot run a command on your machine unasked.** A
project's hooks run only after you trust them. tofu pins each one by a hash
of its entry and of the script it runs, and asks again when either changes.
Hooks in your home are yours and run as they are.

**A hook can act on Jev's verdict, and only tofu has one.** `GateVerdict`
fires after the gate decides on a call, with the verdict, the risk that
decided it and every answer. A hook can turn an ask into deny or allow, a
deny into ask, or tighten any verdict. It can never turn a deny into allow,
and it never touches a call only you may answer.

**A hook can shape a sub-agent before it starts.** `SubagentSpawn` fires
with the definition, mission and owns. A hook can refuse the spawn or
narrow owns, never widen them: a glob tofu cannot prove is inside what was
asked refuses the spawn.

**A hook's reply is checked, not trusted.** Each event accepts its own
fields. A misspelt or foreign field rejects the whole reply, and the
problem shows in `tofu session trace` and `tofu hooks` instead of the hook
quietly doing nothing.

**Every run is in the session.** `tofu session trace` lists each hook run
beside its call: the event, exit code, duration, decision, command, source,
and standard error with keys masked.

**A hook cannot take the machine with it.** At most 4 hook processes run at
once across every tofu and every sub-agent. A hook that never exits is
killed with everything it started when its timeout passes, 60 seconds when
it names none, and a `Stop` hook can keep a turn going at most 8 times in a
row.

## Using hooks

- **See every hook, its trust and its last result**: `tofu hooks`.
- **Trust this project's hooks**: answer the question the app asks when a
  turn starts, or run `tofu hooks trust`.
- **Refuse a hook**: answer deny. It stays refused until it changes.
- **Turn every hook off in a project**: `"disableAllHooks": true` in
  `.claude/settings.local.json`.

A hook runs under bash unless it says `"shell": "powershell"`. On a machine
with no bash it is not run, and `tofu hooks` says why rather than running
bash syntax under PowerShell.

## Events

| Event | When | What exit 2 does |
|---|---|---|
| `PreToolUse` | before a tool call | refuses the call, and the model reads why |
| `PostToolUse` | after a tool call | puts the reason beside the result |
| `UserPromptSubmit` | when a prompt starts a turn | refuses the prompt |
| `Stop` | when the model answers with no tool call | keeps the turn going |
| `SubagentStop` | when a sub-agent answers with no tool call | keeps the sub-agent going |
| `SessionStart` | the first turn after startup, resume or clear, and after a compaction | nothing; its output becomes context |
| `SessionEnd` | the app or `tofu drive` exits, or `tofu run` finishes | nothing; it has 1.5 seconds |
| `GateVerdict` | after Jev's gate decides on a call | the verdict becomes deny |
| `SubagentSpawn` | before a sub-agent starts | refuses the spawn |

A `GateVerdict` hook that turns Jev's ask on `rm` into deny, in
`.tofu/hooks.json`:

```json
{"hooks": {"GateVerdict": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "bash .tofu/no-rm.sh"}]}]}}
```

```sh
in=$(cat)
echo "$in" | grep -q '"verdict":"ask"' || exit 0
echo "$in" | grep -q '"command":"rm ' || exit 0
echo '{"hookSpecificOutput":{"hookEventName":"GateVerdict","permissionDecision":"deny","permissionDecisionReason":"no rm in this project"}}'
```

A `SubagentSpawn` hook that keeps every sub-agent inside `src/`:

```json
{"hooks": {"SubagentSpawn": [{"hooks": [{"type": "command", "command": "echo '{\"hookSpecificOutput\":{\"hookEventName\":\"SubagentSpawn\",\"owns\":[\"src/**\"]}}'"}]}]}}
```

What `tofu session trace` showed for both, in a real session:

```text
hooks
  ✓ orchestrator  toolu_01YLHGkHZHhqS5B2ocwFr2wX spawn  SubagentSpawn  exit 0
    in 310 ms  owns [src/**]                bash .tofu/narrow.sh · project
  ✓ orchestrator  toolu_01VX9oUy1dQoPPtto4nD8Dh4 bash   GateVerdict    exit 0
    in 398 ms  deny: no rm in this project  bash .tofu/no-rm.sh · project
```

## Commands

```sh
tofu hooks
```

```text
Hooks · 3 hooks

  ✓ PostToolUse  Edit  trusted  grep -o '"file_path":"[^"]*"' >>
    "$CLAUDE_PROJECT_DIR/edits.log"
    .claude\settings.json · project · last: exit 0 in 131 ms
  ✓ PreToolUse   Bash  trusted  echo 'shell commands are refused in this
    project: use read and edit' >&2; exit 2
    .claude\settings.json · project · last: exit 2 in 155 ms
  ✓ Stop         *     trusted  if [ -f stop-once ]; then exit 0; fi; ...
    .claude\settings.json · project · last: exit 2 in 127 ms
```

`tofu hooks trust` trusts every project hook that is not trusted yet. Both
take `--json`.
