---
topic: tool
title: One tool call by hand
summary: list the tools a run offers and run one call the way the loop runs it, printing the exact text the model would receive
verbs: tool
---

## What it is

`tofu tool` runs one tool call outside a turn, through the same tool set
and the same call path a turn uses, and prints the result text the model
would receive, byte for byte. No model is asked. The same command on the
same files prints the same output, so a property of a tool is a command a
person reruns.

Each call is one call in a fresh turn. A second `read` of the same file
is whole again, a `plan` holds only what that call set, and `bash` with
`background: true` and `shell` are refused with `no shell registry is
attached to this turn`. An old tool name that is now a mode of `history`,
such as `quote`, is refused with the call to make instead.

## Where it lives

The call runs in `--dir`, or the current directory without it, and reads
the tofu state for that project from your home, `~/.tofu`. Standard error
ends with one line naming each file the call created, changed or removed
in that project's state folder or among the files directly inside
`~/.tofu`, or saying nothing was written. The state folders of other
projects are not looked at.

## Change it

- `tofu tool <name> '<json>' [--dir <dir>]` runs one call. The result goes
  to standard output exactly as the model reads it: the shell sift's cut of
  a bash result, a PostToolUse hook's words, the artifact notice of a
  result over the result cap, and the empty-result sentence all included.
- `--agent <name>` runs the call as a spawn of that sub-agent runs it,
  inside the spawn's ownership boundary. A spawn here holds no owns, so a
  write, edit or bash that writes is refused with the words a sub-agent
  gets.
- `--session <name|id|head>` binds the call to a recorded session, so
  `history` answers from it in every mode: `quote`, `recall`, `zoom` and
  `lookup`.
- `--sift free|judged` picks the arm that cuts a bash result, as
  `tofu run --sift` does; without it the method table decides.

## Check it

- `tofu tool list [--dir <dir>]` prints the names a run offers the lead,
  one per line, in the order the request sends them. It names the same
  tools, in the same order, as `tofu run --show-prompt`.
- `tofu tool list --agent <name>` prints the names a spawn of that
  sub-agent is offered.
- `tofu docs tools` says what each tool returns.

The exit code says how the call went:

- 0: the tool ran and returned a result.
- 1: the tool failed; standard output holds the same error text the model
  gets, starting `error: `.
- 2: a usage error. A name the run does not offer is refused with the names
  it does offer; malformed JSON is refused with the byte where it broke;
  `spawn`, `message`, `subagents` and `ask` are refused because they answer
  only inside a live turn, with a model and a running session.

## Undo it

A call that writes, such as `write`, `edit` or `bash`, changes your files
the way the model's call would. Run it on a copy of the project first, or
use `whatif`, which applies edits to copies of the files and leaves the
working tree as it was; see `tofu docs turn-history`.
