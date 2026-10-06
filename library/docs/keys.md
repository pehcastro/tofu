---
topic: keys
title: Keys
summary: the keys the app answers to, which ones you can rebind, and finding a prompt you sent before with ctrl+p
---

## What it is

The app is driven from the keyboard, and the mouse is never required. Most
keys do the same thing everywhere; a few work only in the composer, on one
tab, or in a dialog.

Seven actions can be rebound:

- Search: `ctrl+k`
- Commands: `alt+k`
- Settings: unbound
- Models: `ctrl+l`
- Quote selection: `ctrl+r`
- Edit in editor: `ctrl+g`
- Prompt history: `ctrl+p`

`ctrl+c`, `ctrl+v`, `ctrl+j` and `alt+i` belong to the composer and cannot
be given to an action.

Prompt history is every prompt you sent from the app, in every session and
every project, newest first. `ctrl+p` opens a search over it: type part of
a prompt to filter, `up` and `down` to move, `enter` to put the prompt in
the composer, and `esc` to close. `enter` never sends it; read it, change
it, and press `enter` in the composer when it is right. A long prompt comes
back as the same text chip it was sent as.

`up` and `down` in an empty composer walk the prompts of the session you
are in, without a search.

`ctrl+c` while the lead works stops the lead at once, and its sub-agents
keep running. With the lead idle and sub-agents working, the first press
shows "this will stop 2 sub-agents, press Ctrl+C again to confirm" for 3
seconds; a second press in that time stops them, and otherwise nothing
stops. With nothing running, a second press quits tofu.

## Where it lives

- `~/.tofu/keybindings.json`: the actions you rebound. Only what you
  changed is needed; the rest keep their default.
- `~/.tofu/prompts.jsonl`: the prompt history, one prompt a line.

The search shows the newest 1000 prompts. When the file grows past 2 MB,
the oldest prompts are dropped until it is back under 1 MB. A prompt that repeats the one before it is
kept once. A prompt over 16 KB is not kept, since half a prompt sent by
mistake is worse than none. A key tofu knows about is replaced by
`[key redacted]` before the line is written, the same way the session log
redacts it. Slash commands are not kept.

A line that cannot be read is skipped and the rest still loads, and a
missing file is an empty history. Two apps open at once both write to the
same file.

## Change it

In the app, open the Settings tab, pick the Keybindings row and press
`enter`. Pick an action, press `enter`, and press the new key. A key
another action holds is refused. The change is saved at once.

A keybindings file written before Prompt history existed keeps working: if
it already gives `ctrl+p` to another action, that action keeps it and
Prompt history is left unbound until you give it a key.

## Check it

    /keys

in the app lists every key it answers to, grouped by where it works, with
the key each rebindable action is bound to now. Type to filter it, for
example `history`.

Press `ctrl+p` in the chat: the dialog titled Prompt history opens, or says
"no prompt sent yet" when the history is empty.

## Undo it

Delete a line from `~/.tofu/keybindings.json` to put that action back on
its default, or delete the file to put every action back.

Delete `~/.tofu/prompts.jsonl` to forget every prompt; the next prompt you
send starts a new one. Removing one prompt means removing its line from the
file with an editor while the app is closed.
