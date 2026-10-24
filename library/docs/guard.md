---
topic: guard
title: Guard rules
summary: the code rules a repository's Claude Code and git hooks enforce, run by tofu itself on Windows, Linux and macOS, with the same answers the old PowerShell scripts gave
verbs: guard
---

## What it is

`tofu guard` runs one hook rule and answers the way a Claude Code or git
hook answers: exit 0 to let the call through, exit 2 with a reason on
standard error to refuse it, or a `permissionDecision` deny on standard
output. A Claude Code rule reads the hook's JSON on standard input; a git
rule reads the commit message file git hands it.

The rules, by name:

- `emdash`: refuses a write carrying an em dash.
- `comments`: refuses a Go file holding a comment; compiler directives pass.
- `library-wording`: refuses a write under `library/` that names a ticket,
  a dated report or a decision attributed to a person.
- `shell`: refuses a shell command that writes source around the Write and
  Edit tools, discards uncommitted work, changes git from a sub-agent, adds
  a worktree, runs `go test` over a pattern without `-p`, or runs a
  tree-wide check while a ticket in doing owns a Go path. It notes a commit.
- `loc-track` and `minify-gate`: record each Go file written, and refuse
  the first stop until a minify pass names them.
- `snapshot`: copies each tracked file written to the user cache folder,
  twenty copies a path at most.
- `require-ticket`, `own-paths`, `ticket-log`, `research-used`: the ticket
  rules. A spawn cites a live ticket with Acceptance, owns, a matching
  owner and no overlap with a doing ticket; a sub-agent writes only a path
  a doing ticket owns; a ticket leaves review only with a Log. A read-only
  agent, one whose `tools:` hold no edit or patch tool, starts with no
  ticket while its spawn names no ticket and no owns, and the refusal
  names the read-only agents the project has.
- `git commit-msg` and `git release-docs`: a conventional subject of at
  most 72 characters with no attribution trailer, and a release whose
  changelog names are all in `docs/`.

## Where it lives

The rules are compiled into the tofu binary, so a hook needs no PowerShell,
bash or Python. One process start answers each call on every OS.

Paths come from the system rather than from a variable: the minify list in
the temporary folder, `os.TempDir`, and snapshots under the user cache
folder, `os.UserCacheDir`, in `boji/snapshots/<project>`.

The ticket rules read the project's boards when `projectManagement` is
`boardy` and the project has a board. Otherwise they read the project's
ticket folders, `open`, `doing`, `review`, `done` and `dropped`.

## Change it

Point a Claude Code hook at a rule in `.claude/settings.json`:

    "command": "tofu guard emdash"

Point git's commit-msg hook at the message rule, in `.githooks/commit-msg`:

    #!/bin/sh
    exec tofu guard git commit-msg "$1"

Turn the ticket rules over to boards:

    tofu settings set projectManagement boardy

## Check it

Feed a rule the event a hook would get:

    echo '{"tool_input":{"command":"git worktree add ../x"}}' | tofu guard shell

prints a deny whose reason opens `git worktree is refused in this
repository.` and exits 0, as a Claude Code hook does.

    tofu guard git commit-msg .git/COMMIT_EDITMSG

exits 1 and names the reason, such as `commit refused: the subject is 73
characters, the limit is 72.`

An unknown rule exits 2 and lists the rules, so a hook pointed at a typo
refuses rather than lets everything through.

## Undo it

Remove the hook entry from `.claude/settings.json`, or point it back at the
script it replaced. Nothing a rule writes outlives its purpose: the minify
list is deleted at the stop it names, and snapshots beyond twenty a path
are removed as new ones arrive.

    tofu settings set projectManagement off

returns the ticket rules to the folders.
