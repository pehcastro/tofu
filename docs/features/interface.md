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

- **Change a shortcut**: Search, Commands, Models, Quote selection and
  Settings can be rebound; they're saved in `~/.tofu/keybindings.json`.
  `Ctrl+C`, `Ctrl+V`, `Ctrl+J` and `Alt+I` belong to the composer.
- **Paste an image or a file**: `Ctrl+V` attaches what the clipboard holds.
  On Linux it reads through `wl-paste` on Wayland, or `xclip` or `xsel` on
  X11, so install `wl-clipboard` or `xclip` first.
- **Answer the gate** when `gatePrompt` is `ask`: `1` allows once, `2`
  refuses, `3` always allows that tool on that file this session.
- **Open on the last session**: `tofu --continue`.

## Keys

| Key | Does |
|---|---|
| `Tab`, `Alt+1` to `Alt+4` | Next tab, or jump to one |
| `Alt+K` | Command palette |
| `Ctrl+K` | Search |
| `Ctrl+L` | Pick the model |
| `Shift+Tab` | Cycle the effort level |
| `@` | Attach a file |
| `Ctrl+O` | Expand the last tool call |
| `Ctrl+Y`, `Alt+Y` | Copy the last answer, or the last tool call |
| `Ctrl+X`, `Alt+Up`, `Alt+Down` | Remove or pick a queued message |
| `Esc` | Stop the turn |
| `Ctrl+C` twice | Quit |
