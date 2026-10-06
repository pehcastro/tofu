---
title: The interface
description: "The tofu app in the terminal: four tabs, a command palette, and a composer that keeps working while the model does."
order: 10
updated: 2026-10-05
---

Run `tofu` in a project to open the app. Four tabs hold the work:

- **chat**: the conversation with the lead and the composer
- **sub-agents**: every agent by state, and each one's thinking and tool calls
- **file edits**: every changed line, who changed it, and where
- **shells**: the processes an agent left running

![Alt+K, the command palette](./media/tui-commands.png)

`/` opens the slash commands, and each one is also a `tofu` verb. On first
start, a setup screen lists anything missing, such as a sign-in, with a
number to fix each.

## Why the chat stays clean

**The chat is for you, the tabs are for the detail.** Tool calls stay out of
the chat by default, so what you read is the lead's answer, and every call is
one tab away.

**You never wait on the model to type.** A message sent mid-turn reaches the
model at its next step.

**It stays fast.** Resizing redraws in 0.35 to 3.24 ms a frame, down from 97
to 169 ms, and the sub-agents tab opens in 2.0 ms, down from 59.6 ms, all
inside the 16.7 ms of a 60 Hz frame.

## Shortcuts and the gate

- **Write the prompt in your own editor**: `Ctrl+G` opens what you have
  typed in `$VISUAL`, else `$EDITOR`, else notepad on Windows and vi
  elsewhere. Save and quit, and the text comes back to the composer.
  `code --wait` and other commands with arguments work.
- **Find a prompt you sent before**: `Ctrl+P` searches every prompt you
  sent from the app, in every session and project, newest first. Type part
  of it, press `Enter`, and it lands in the composer without being sent.
  The history lives in `~/.tofu/prompts.jsonl`, with known keys redacted.
- **Run a command without asking the model**: a prompt that starts with
  `!` runs the rest in the project, the way the `bash` tool does, and costs
  no request. `!git status` shows its output in the chat, and your next
  prompt carries the command and its output to the model. `Esc` stops a
  command; a stopped one is not carried. Known keys in the output are
  redacted.
- **Change a shortcut**: Search, Commands, Models, Quote selection, Edit in
  editor, Prompt history and Settings can be rebound; they're saved in
  `~/.tofu/keybindings.json`.
  `Ctrl+C`, `Ctrl+V`, `Ctrl+J` and `Alt+I` belong to the composer.
- **Paste an image or a file**: `Ctrl+V` attaches what the clipboard holds.
  On Linux it reads through `wl-paste` on Wayland, or `xclip` or `xsel` on
  X11, so install `wl-clipboard` or `xclip` first.
- **Drop a screenshot on the terminal**: a dragged or pasted path to a png,
  jpg, gif or webp file attaches the image. Any other path stays text.
- **Answer the gate** when `gatePrompt` is `ask`: `1` allows once, `2`
  refuses, `3` always allows that tool on that file this session.
- **See a sub-agent that stopped moving**: a sub-agent whose tool call has
  been open for 11 minutes, a minute past the longest bash deadline, is
  shown as stalled under the turn line, with the call and how long it has
  been open. The call says so on the Sub-agents tab too.
- **Open on the last session**: `tofu --continue`.
- **Go back to any session without leaving**: `/resume` lists this
  project's sessions, newest first, with the one in use marked. Type a word
  of its name or first line, press `Enter`, and the chat shows that session
  and the next task carries it. `/resume <name or id>` skips the list.

## Keys

`/keys` lists every key the app answers to, grouped by where it works, with
the key each rebindable action is bound to now. Type to filter it.

| Key | Does |
|---|---|
| `Tab`, `Alt+1` to `Alt+4` | Next tab, or jump to one |
| `Alt+K` | Command palette |
| `Ctrl+K` | Search |
| `Ctrl+L` | Pick the model |
| `Shift+Tab` | Cycle the effort level |
| `@` | Attach a file |
| `Ctrl+G` | Edit the prompt in your editor |
| `Ctrl+P` | Search the prompts you sent, in every session |
| `Ctrl+O` | Expand the last tool call |
| `Ctrl+Y`, `Alt+Y` | Copy the last answer, or the last tool call |
| `Ctrl+X`, `Alt+Up`, `Alt+Down` | Remove or pick a queued message |
| `!` at the start | Run a shell command, read by the next prompt |
| `Esc` | Stop the turn, or a running `!` command |
| `Ctrl+C` | Stop the lead at once; its sub-agents keep running |
| `Ctrl+C` twice in 3 s, lead idle | Stop the running sub-agents, after a line naming how many |
| `Ctrl+C` twice, nothing running | Quit |
