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

`shift+enter`, `alt+enter` and `ctrl+j` add a line. The composer grows to
8 rows and then scrolls, keeping every line you typed.

`@` at the start of a word is typed like any other character and lists the
files matching what follows it under the input, with no dialog; `enter`
writes `@path`.

Dragging over the chat selects text rather than screen cells: while the
button is held the wheel scrolls, and the pointer held on the top or bottom
edge scrolls by itself. Letting go copies every line from where you pressed
to where you let go, the scrolled ones too, and `ctrl+c` copies the same
while you still hold it.

A pasted image, a long pasted text and a paste still loading each sit in
the composer as one chip, such as `[Image #1]`. `backspace` or `delete`
on any part of a chip removes the whole chip, and the arrow keys step over
it in one move, so a chip is never left half typed. Before an image is
sent, tofu fits it to at most 1568 pixels on its long edge, about 1.15
million pixels and 5 MB, whether you pasted it, a client sent it or the
model read it from a file. A webp over 5 MB cannot be shrunk and is
refused with a note to convert it to png or jpeg. A sent message says
`Attached 1 Image` or `Attached 3 Images` under its text, and each pasted
text keeps its own line, such as `text, 2400 characters`.

The lead's work between two messages is one row in the chat: `running bash · (56) steps · 3 agents` while it works, `finished · (70) steps · 3 agents · 32m 12s` once settled. A commit, a push and a ticket created or moved each get one line, such as `◆ 3f2a91c · fix the parser` or `▣ DASH-350 · moved todo > doing`, a move that changed nothing draws none, and every other call is counted; `ctrl+o` or a click lists those calls under the row, with `collapse` to fold it. Running sub-agents share one row per status, `working`, `waiting for answer` or `reopened`, such as `working · [&ada], [&bo]`, and one that finished leaves the row.

The line under the chat names what the lead is doing, `requesting`, `thinking`, `working`, `spawning`, `waiting` on you or `stopping`, with the time in it and the request id, such as `thinking  |  12s  [request#c1a2b3]`. With the lead idle while sub-agents run it reads `waiting  |  3 sub-agents · 2 in bash`, and `reading the reports` once none runs. After a turn it says how long the turn took and how long it waited on the model's first replies: `cooked for 3s  |  waited 1s`.

`ctrl+c` while the lead works stops the lead at once, and its sub-agents
keep running. With the lead idle and sub-agents working, the first press
shows "this will stop 2 sub-agents, press Ctrl+C again to confirm" for 3
seconds; a second press in that time stops them, and otherwise nothing
stops. With nothing running, or only tool calls finishing, a second press
quits tofu.

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
