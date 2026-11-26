---
title: The interface
description: "The tofu app in the terminal: four tabs, a command palette, and a composer that keeps working while the model does."
order: 10
updated: 2026-11-25
---

Run `tofu` in a project to open the app. Four tabs hold the work, and `/` with a tab's name opens it:

- **chat**: the conversation with the lead and the composer
- **sub-agents**: every agent by state, and each one's thinking and tool calls
- **file edits**: every changed line, who changed it, and where
- **shells**: the processes an agent left running

![Alt+K, the command palette](./media/tui-commands.png)

`/` opens the slash commands. On first
start, a setup screen lists anything missing, such as a sign-in, with a
number to fix each.

## Why the chat stays clean

**The chat is for you, the tabs are for the detail.** The lead's work
between two messages is one row, so what you read is the lead's answer, and
every call is one tab away. While the lead works, the row says what it does
now and counts the steps, such as `running bash · (56) steps · 3 agents`;
once it settles, it reads `finished · (70) steps · 3 agents · 32m 12s`. A
commit, a push and a ticket created or moved each get one line of their own,
such as `◆ 3f2a91c · fix the parser` or `▣ DASH-350 · moved todo > doing`;
every other call is only counted, and a move that changed nothing draws no
line. Click the row or press `Ctrl+O` and it lists those calls under it,
with `collapse` to fold it again.
Running sub-agents share one row per status, `working`, `waiting for answer`
or `reopened`, such as `working · [&ada], [&bo]`, and one that finished
leaves the row.

The line under the chat says what the lead is doing, for how long, and the
request it is on: `requesting`, `thinking`, `working`, `spawning`, `waiting`
on you or `stopping`, such as `thinking  |  12s  [request#c1a2b3]`. While
the lead is idle and sub-agents run, it reads `waiting  |  3 sub-agents · 2 in
bash`, and once none runs, `reading the reports`. After a turn it says how long the turn took and how long it waited
on the model's first replies, such as `cooked for 3s  |  waited 1s`. An
answer streams without stutter and is never cut in two: a code block draws
plain while it is typed and gets its colours when its closing fence arrives.

**You never wait on the model to type.** A message sent mid-turn reaches the
model at its next step.

**It stays fast.** Resizing redraws in 0.35 to 3.24 ms a frame, down from 97
to 169 ms, and the sub-agents tab opens in 2.0 ms, down from 59.6 ms, all
inside the 16.7 ms of a 60 Hz frame.

**It opens at once.** The first screen draws in about 81 ms on a fresh start
and 104 ms when `tofu --continue` resumes a 2,130-event session, where it took
184 and 285 ms. tofu opens the credential store once, reads only what the
resumed session shows, and loads the time zone and git identity beside the
first frame.

**It costs nothing while you read.** Sitting idle, tofu uses about 2.4% of a
core in a chat and 1.1% on the setup screen, under half of what it used: the
input reader waits on the console instead of polling, the screen redraws only
when something changed, and the welcome animations share one clock.

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
- **Copy an answer longer than the screen**: drag over the chat. The
  selection holds on to the text rather than the screen, so while you hold
  the button the wheel scrolls it, and the pointer held on the top or bottom
  edge scrolls by itself. Letting go copies every line from where you
  pressed to where you let go, the scrolled ones too; `Ctrl+C` copies the
  same while you still hold it.
- **Change a shortcut**: Search, Commands, Models, Quote selection, Edit in
  editor, Prompt history and Settings can be rebound; they're saved in
  `~/.tofu/keybindings.json`.
  `Ctrl+C`, `Ctrl+V`, `Ctrl+J` and `Alt+I` belong to the composer.
- **Paste an image or a file**: `Ctrl+V` attaches what the clipboard holds.
  On Linux it reads through `wl-paste` on Wayland, or `xclip` or `xsel` on
  X11, so install `wl-clipboard` or `xclip` first. The image reaches the
  model with the message it is in, also when that message waits for the
  next step while sub-agents run, and after a fork. Before it is sent,
  tofu fits every image to at most 1568 pixels on its long edge, about
  1.15 million pixels and 5 MB, whether you pasted it, a client sent it
  or the model read it from a file; a webp over 5 MB is refused with a
  note to convert it.
- **Edit around a chip**: a pasted image, a long pasted text and a paste
  still loading each sit in the composer as one chip, such as
  `[Image #1]`. `Backspace` or `Delete` on any part of it removes the
  whole chip, and the arrow keys step over it in one move. Once sent, the
  message says `Attached 1 Image` or `Attached 3 Images` under its text,
  and each pasted text keeps its own line, such as `text, 2400 characters`.
- **Drop a screenshot on the terminal**: a dragged or pasted path to a png,
  jpg, gif or webp file attaches the image. Any other path stays text.
- **Answer the gate** when a call waits for you: `1` allows once, `2`
  denies, `3` always allows it here, `4` never allows it here, and `5`
  cancels the turn. See [Asking you](/docs/features/asking).
- **Answer the lead's question**: when the lead asks with options, a form
  opens above the composer with its recommended option marked. A digit or
  `Enter` chooses, **other** takes your own words, and in auto the work
  keeps going while you decide.
- **See the quota**: the footer names the account that served the last
  request and shows the fullest subscription window, and `read 12m ago`
  beside a stale reading. See [Subscription
  quota](/docs/llms/quota).
- **See how fast the model writes**: while text streams, the footer shows
  the output rate, such as `128.4 tok/s`. It counts the last 20 seconds
  most, the way [oh-my-pi](https://github.com/can1357/oh-my-pi) does, and
  shows nothing under 200 tokens or 4 seconds of output. After the request
  it keeps the request's own rate in grey, output tokens over the time from
  send to the last byte, through its tool calls and the next request.
  While sub-agents stream, `sub-agents 412.0 tok/s` beside it sums their
  rates, and drops when the footer has no room.
- **Leave it in another tab**: tofu reports working, waiting on you, done or
  failed to the terminal over OSC 7501, so a terminal that shows it tells
  you when a turn needs you. See [Program status](/docs/features/status).
- **See what each sub-agent is doing**: under the turn line, a sub-agent
  waiting on the lead, or with a call open longer than
  `subAgentWatchSeconds` (600), says what it is doing: `waiting for the
  orchestrator's answer, 5m 30s`, `running bash, 12m` or `building, 12m`.
  It reads `stalled` only when no output, step or request has moved for
  that long, and the call says so on the sub-agents tab too.
- **Point at a message**: each message shows the id it was recorded under,
  such as `[message#9c2d40]`. Type it in a prompt and the lead quotes that
  message: its words arrive with your prompt in a `quote` notice, so the
  lead answers from them without looking them up.
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
| `Shift+Enter`, `Alt+Enter`, `Ctrl+J` | New line; the composer grows to 8 rows, then scrolls |
| `@` | At a word start, list matching files under the input; enter writes `@path` |
| `Ctrl+G` | Edit the prompt in your editor |
| `Ctrl+P` | Search the prompts you sent, in every session |
| `Ctrl+O` | Open the lead's last row, or the last tool call |
| `Ctrl+Y`, `Alt+Y` | Copy the last answer, or the last tool call |
| `Ctrl+X`, `Alt+Up`, `Alt+Down` | Remove or pick a queued message |
| `!` at the start | Run a shell command, read by the next prompt |
| `Esc` | Stop the turn, or a running `!` command |
| `Ctrl+C` | Stop the lead at once; its sub-agents keep running |
| `Ctrl+C` twice in 3 s, lead idle | Stop the running sub-agents, after a line naming how many |
| `Ctrl+C` twice, nothing running | Quit |
