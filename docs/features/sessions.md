---
title: Sessions
description: Every session is recorded as it runs, outside your project, and one command reopens it where you left off.
order: 6
updated: 2026-10-28
---

A session is one conversation with tofu: every message, tool call and
sub-agent, appended to disk as it happens. It lives in your home, under
`~/.tofu/projects/<folder>-<hash>/sessions/<id>/`, as `session.json` (the header:
task, model, outcome, tokens, cost, sub-agents) and `events.jsonl` (one line
per event). A new conversation gets a three-word name and a family tag,
like `brisk-amber-heron#4mhr1.1`, and keeps both for its whole life.

![tofu --continue reopening the last session](./media/tui-continue.png)

## Why sessions live outside the project

**Nothing lands in your project.** No `.tofu` folder of logs to ignore or
commit by mistake.

**Appended, not saved at the end,** so a crash keeps everything up to the
last event, and `tofu --continue` picks up from there.

**A cut reply is kept.** A reply cut short by `Esc`, a closed terminal or a
hangup is saved as far as it got, and `tofu --continue` shows it marked
interrupted. Quitting waits at most 3 s for it. On Linux and macOS a hangup
quits tofu the way a person's quit does, saving first.

**Never rewritten.** When a session forks to stay under its context budget,
the old one keeps its whole record and points at the new one, and the new
one carries every message you typed, word for word.

**Deleted only when idle for long.** A session nobody touched for 90 days is
deleted when the project's tofu host starts; see
[Cleaning up old sessions](#cleaning-up-old-sessions).

**One name for one conversation.** A fork to stay under the context
budget, `/compact` and a move to another account start the next
generation of the same family: the name stays and the number after the
`.` rises, so `brisk-amber-heron#4mhr1.3` is the third. `/new` starts a new
family. A branch, another way from a point, is a new family that records
where it branched. A name from an older tofu still finds the generation
that carried it, and hooks see the family id as `session_id`.

**Every message has an id you can point at.** The chat shows each message,
yours, the lead's and each sub-agent report, with the id it was recorded
under, such as `[message#9c2d40]`. Type that reference and the lead quotes
the message itself rather than its memory of it, across forks and
`--continue`. `tofu session trace` lists the same ids.

**A continued session opens as it was.** Each session file is read once,
so `tofu --continue` opens in under a second on a chain of 21 sessions
with 84 sub-agents. Each message keeps its own time, each sub-agent report
draws as a report, and a conversation over the context target forks
before its first request instead of resending the whole history. A resumed
session and a resumed sub-agent keep their pictures, their model and the
sub-agent's thinking, and a fork keeps the model you chose. A resumed
session draws its chat across every earlier generation, so scrolling up
reaches the first message, in the app and over `tofu serve`.

**A long session stays light.** The chat holds a window of the newest rows
and reads older ones from the session as you scroll, so a session of
thousands of rows scrolls and types as fast as a new one. Past 25 picture
results, tofu drops all but the newest 20 at once, so the provider's prompt
cache rebuilds once rather than on every turn.

## One host per project

Every session runs in the project's `tofu host`, one process per project
that the terminal, the desk and `tofu serve` each join as a member. No
window owns a session, so closing one leaves the others working. A prompt,
`Esc`, a steer or an approval you give in any member runs in the host, and
the first answer to a question wins. A member that joins late gets the
transcript and then every event live, token by token.

You never start the host yourself. The first window starts it, and it
exits 5 s after its last member leaves. When the last member leaves, the
host stops the session's turn, naming that member, and ends its shells. A
member that stops answering for 10 s is let go.

```sh
tofu host status
tofu host ensure
tofu host stop
```

`status` prints the host's process, socket, project, version, members and
start time; `ensure` starts it when none answers; `stop` ends it now. Each
takes `--dir` and `--json`. The host's log is `~/.tofu/h/<key>.log`, kept
under 1 MiB with one older copy beside it.

The members and the host talk over a local socket under your home, or, on
Windows, under a temp folder only your user can open, so nothing is
reachable from another machine or another user. A newer tofu that joins a
host started by an older one says so, and the session runs on the older
build until its last member leaves.

## Side chats

A side chat is a second conversation beside the lead, for a question you
don't want in the main thread: what a function does, a draft of release
notes, a look at a log. It starts from a summary of the session it branches
from, answers while the lead's own turn keeps running, and never becomes the
session `tofu --continue` opens.

**It reads by default.** A side chat writes nothing unless you say so, so
it can't touch the files the lead's sub-agents are changing. The `notes`
preset lets it write `.md` and `.html` files, `files` lets it write
anywhere, and `--owns` names exact globs. Whatever its access, a side chat
never spawns sub-agents, schedules a job, remembers anything or changes a
setting. The `sideChatAccess` setting picks the default, `read`.

```sh
tofu session branch brisk-amber-heron --side
tofu session branch brisk-amber-heron --side --preset notes
tofu session branch brisk-amber-heron --side --owns "docs/**" --seed none
```

In the app, `/side` opens one beside the session you are in, with the
`sideChatAccess` access and the summary, and the app switches to it.

`--seed none` starts it with no summary. `tofu session list` hides side
chats; `tofu session list --all` shows them. A frontend opens one over
`tofu serve` with `session.branch` and changes its access with
`session.access`.

**It reads the lead's cache.** A side chat sends the lead's system prompt
and tool list byte for byte, so the provider reuses the cache the lead
already paid for. What makes it a side chat comes after that cached head,
in one message: that it sits beside the lead, what it may write, and the
rules its `bash` runs under. `spawn`, `message`, `subagents`, `cron`,
`remember` and the other tools only the lead uses stay in the list and
refuse when called, saying why. `tofu session request` on the lead's last
request and the side chat's first shows the same system text and the same
tools.

## Continuing, naming and removing

- **Continue the last session**: `tofu --continue`, or `/resume` in the app.
- **Continue another**: `tofu session resume <name|id|head>`.
- **Start fresh**: `/new`.
- **Name one**: `tofu session rename <name|id|head> "checkout redesign"` makes it
  `checkout-redesign`, on every generation.
- **Point at one generation**: `brisk-amber-heron#4mhr1.2`; the name alone
  is the newest.
- **Change its model**: `tofu session set <name|id|head> --model
  claude-sub/claude-sonnet-5` runs it on that model from its next resume, or
  from its next turn when a host holds it.
- **Delete one**: remove its folder under `sessions/`, or let
  `tofu session clean` remove the old ones.
- **Move sessions an older tofu left in the project**: opening the app moves
  them once and prints one line; `tofu migrate --dry-run` lists the move
  first. A file the home already holds with other bytes is settled by kind:
  `sessions/HEAD` and `cache/` keep the home's, a `log/` day gains the old
  copy's missing lines in time order, and any other file keeps the old copy
  beside it as `<name>.repo`. The project's `settings.json` stays.
- **Move or rename a repository**: open it in the new place. tofu knows it by
  its git remote or first commit and offers to relink its sessions; the app
  asks, and `tofu migrate --relink` accepts from the command line.

`<folder>-<hash>` is the project folder's name and eight characters of a hash
of its full path, read after links and Windows junctions, so `C:\code\shop`
is `shop-7cd21bfb` and two folders never share a record. A folder from an
older tofu, named after the whole path, is renamed to the new name the first
time the project opens, instantly whatever its size, and nothing is left under
the old name. If an older tofu still holds a file in it, the open says so in
one line, keeps reading the old folder and tries again next time; close older
tofu sessions before that first run.

## Cleaning up old sessions

A session idle for longer than the `sessionStaleDays` setting, 90 days by
default, is deleted when the project's host starts. Idle means nothing in
its folder changed. `0` keeps every session forever.

```sh
tofu session clean --dry-run
tofu session clean
```

`--dry-run` lists what would go and why the rest stays. tofu always keeps
the head, a session that a kept one continues or branches from, a session
open or busy now, and one with a cron job that still schedules.
`tofu session list` counts the sessions idle past the lifetime.

## Commands

```sh
tofu session list
```

```text
Sessions · 123 sessions                                 ● head brisk-amber-heron

  ● brisk-amber-heron     26 Sep 14:26  5 steps   stopped         explain this
    repository to me
```

One row per conversation. Every session command takes `head` in place of
a name or an id, for the session `●` marks: `tofu session trace head`,
`tofu session request head <request id>`. In a project with no session,
`head` says `no session recorded here` and points at `tofu session list`.

`tofu session <name>` prints the family: when it
started, how long it was worked on, counting each turn from its first
event to its last, and every generation with how it began, its steps,
active time and size, its branches and its sub-agents. On a conversation
of 80 generations it answers in about 0.7 s.

```sh
tofu session find brisk-amber-heron --tool bash
```

finds every call across every generation, and `--command`, `--file`,
`--text`, `--agent`, `--since` and `--until` narrow it, together. `--text`
also matches a call's result and names the tool it came from. `--failed`
lists only the calls that failed or that a gate refused, a refused call
reading as `bash refused`.

```sh
tofu session trace brisk-amber-heron
tofu session trace brisk-amber-heron --since 1h --agent research-2
tofu session trace brisk-amber-heron --from 04:10 --to 04:12 --kind stop,error
tofu session trace brisk-amber-heron --from person
```

The first prints a summary: lead turns and how they ended, each sub-agent
with its state, calls and last report, errors by kind, every stop with what
it took down, and the commands that drill in. On a generation of 4,758
events it is 44 lines where the full listing is 19,161, and it answers in
0.7 s where the full listing takes 6 s. A filter lists the rows behind it,
one line each, and filters combine: `--since`, `--from` and `--to` (`1h`,
`04:10`, or a date and time), `--agent` (a name, or `lead`), `--turn`,
`--grep`, `--last N`, and `--kind` with any of `turn`, `stop`, `error`,
`spawn`, `report`, `message`, `insert`, `cron`, `request`, `memory`, `call`,
`shell`, `gate`, `hook`, `cache`, `compaction`, `list`, `fork`, `read`,
`startup`, `notice`, `attachment` and `library`. `--kind fork` lists every
fork across the session's generations, each with its agent, the step it
forked after, its messages before and after, and the files it carried
whole, such as `after step 3 · 16 to 5 messages · carried no file whole`;
a fork from an earlier generation says which one recorded it. `list` keeps
every other change to the message list. `--from` also takes an origin, one of
`person`, `agent`, `tofu`, `cron`, `hook`, `memory`, `skill`, `library` and
`plugin`, and lists the messages that origin wrote: `--from person` lists the
task you typed, every message you sent mid-turn, and your answers to
`ask_person`, which tofu records as `tofu/answer` because it words them.
A turn's first row repeats its first message, so `--from` lists turn rows
only beside `--kind turn`, where `--kind turn --from cron` lists the turns a
cron fire began. An unknown origin names the known ones and exits 2. With
`--json` every turn, message and notice row that recorded an origin has
`origin`, with its `kind` and `sub`. `--wide` prints each row whole with its origin, and `--full`
prints every request, call, hook and message section. `--kind notice` lists
each sub-agent move, spawned, reviewed, reopened or accepted, with its round
and reason, such as `go-dev reopened, round 2: acceptance line 3 fails`.

```sh
tofu session work brisk-amber-heron --ticket DEMO-1
```

lists the work done on one ticket in order: the brief the lead gave, the
spawn, the sub-agent's move with `ticket_move`, its end and its whole
report. It reads every generation of the session, so after a fork the
report still names its ticket, even when the spawn was in an earlier
generation. It also lists the board's own moves of that ticket, as `board`
rows: the move to doing at the spawn, a `tofu boardy move` run from the
session's shell, and a board hook's move while the session ran, such as
`board  hook:report_to_review  moved  doing to review`. A board move that a
`ticket_move` row already shows is not listed twice. With `--json` of
`tofu session trace`, the spawn call, the spawn and end rows, the report
row and the `ticket_move` call row carry `ticket` beside `agent`. An id no
row carries prints `no row carries DEMO-9` and exits 0.

`--kind library` lists what the library and your own files gave each agent,
under that agent's name, each with the file it came from:

- every rule that fired, was held back or was dropped, with the reason:
  `rule go_wrap_errors held · a rule for how code is written reaches the
  agent that writes it ...`, or `rule board_lead dropped · the rule's mode
  is off`
- the sub-agent definition used, its references offered or dropped by
  framework or size, and the ones the model read
- every skill offered, loaded by a definition, loaded by the model, or never
  offered and why
- every Jev question set asked in the session's turns, with its verdict, the
  questions, and the set's file, such as `jev tool_gate asked 4 questions,
  allow · ... · library/questions/tool_gate@7.yaml`

A prompt is recorded again only when it changes, so a turn whose prompt
matches the last one adds no rows. A session from an older tofu lists only
what it fired.
A sub-agent that kept writing to the session before a continue shows with
the session it was recorded in.

Every request, call and sub-agent run says when it began as `start_ms`,
milliseconds from the start of the session, so a timeline lines up without
reading timestamps. A message tofu added rather than you typed shows where
it came from as `from <kind>/<sub>`, such as `from tofu/shell.exit` or
`from cron/fire`: every message carries a typed origin, so only your own
words read as you, in the trace, the chat and `tofu serve`.

`tofu session info <name>` prints one session with its model, cost,
context budget, the error it ended on, and its `history`: what it began
with and what it holds now, such as `fresh, holds 40 messages` or
`12 messages from brisk-amber-heron#4mhr1.1, holds 52 messages`, and
`nothing to resume` when it holds none. `tofu session request <name>` with
no request id lists the session's requests, each with its short id, agent,
step, tokens in, cache read and cache written, and `0 requests` when none was
recorded. `tofu session request <name> <request id>`, or `last` for the
newest, prints one request exactly as it was sent with its response or
error body, and a section on what went into it: each prompt part, such as a
rule, an agent or the system prompt, with its tokens and whether it was
switched off, the skills loaded, the memory, the tokens by kind, the images,
and how many messages were new or dropped since the last request. A request
recorded by an older tofu says `not recorded`. Its `cache` line names each
cache breakpoint, such as `the last tool, system block 2, message 13`, and in
the message list a message reads `kept head` when compaction kept everything
up to it and `breakpoint` when it carries one. The breakpoints are read off
the body sent; when no body was kept, as on a recorded model, tofu encodes the
messages through the anthropic wire again and says so, and a wire with no
cache markers says `none`. In `--json` every message carries `kept_head` and
`breakpoint`, and `breakpoints` says where they came from.
`tofu session request <name> --has memory/view` lists every request with
its message count and the positions where a message from that origin sits,
such as `at 1`, or `none`, so one command shows which requests carried a
memory view, a fork carry or a notice. The origin is spelled the way the
message list prints it, `<kind>/<source>`, and an unknown one is refused with
every known origin, exit 2. In `--json` each request carries `messages` and
`at`.
`tofu session reads <name>` prints every file it read. Each takes `--json`,
and `tofu session trace --json` with a filter prints the same rows.
Over `tofu serve`, `session.info` carries the same history as
`carried_from`: the session, its handle and how many messages came over.

```sh
tofu session transcript brisk-amber-heron --from 1 --to 2 --md
```

prints the family's conversation in order: what you said, the lead's
replies, each brief a sub-agent was given and each report it sent back,
with no tool calls and no tool output. `--from N` and `--to N` pick
generations, and `--md` writes it as one markdown file. A report a
sub-agent sent after a fork shows in the generation that received it. A
`--from` past the last generation prints nothing and exits 2. On a session
of two generations and one sub-agent the transcript is 756 bytes where the
trace JSON of the same range is 19,436.

```sh
tofu session drain brisk-amber-heron
```

says where the session's tokens went, by agent, by what woke it, by
context size and by cache rebuild, and which of eight wasteful patterns it
found; see [Where tokens went](/docs/features/drain).
