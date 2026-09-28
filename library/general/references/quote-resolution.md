---
id: quote-resolution
domain: general
document: interface/tui/quote, internal/turn/tools and internal/session, read in this repository
found: this repository; this file is a description of what the code does, not a quotation
also: library/general/references/what-reaches-a-screen.md
---

# How a quote reference resolves

A message can carry `[quote#abcd]`, where `abcd` is the tail of a recorded event id. The `quote` tool turns that into a turn, and the functions under it already name every outcome the rule has to talk about.

## What the tool does

`internal/turn/tools.Quote` takes the reference as the model received it, reads the recorded session it belongs to, and returns that turn's own words, the names of the tools that ran, and nothing else. A tool call's arguments and a tool's output are never handed back, so a credential pasted into a command line is not repeated by the quote. What the person typed comes back as it was recorded, which is the text that was already sent when the turn happened.

A turn larger than the tool result cap comes back cut at the cap, with a note naming its whole size in bytes and how many of them are there. The session record on disk is never written by a quote.

## What the two functions under it do

`interface/tui/quote.Collect` walks a conversation and keeps one `Turn` per utterance: the event id, who said it, and the first non empty line of what was said, or the names of the tools that ran when there was no text. A turn recorded before event ids existed is not skipped. It is given an id derived from the session and the turn's place in it, by `internal/session.EventIDFor`, so an old transcript is quotable on the same terms as a new one.

`interface/tui/quote.Resolve` hands those ids to `internal/session.FindByHash` and returns the turn behind the one that matched. The tool derives its ids the same way and goes through the same `FindByHash`, so the picker and the model read one policy rather than two.

## The three outcomes are the function's own

`FindByHash` counts how many recorded ids the short reference is drawn from and returns one of three things:

- exactly one match: that event
- no match: `ErrEventHashNotFound`, naming the reference
- more than one: `ErrEventHashAmbiguous`, naming the reference and how many events it matched

Nothing falls back to the nearest turn, and there is no fourth case. That is why the rule says an id matching no turn and an id matching more than one resolve to neither, and why it says to name which of the two happened rather than answer anyway: the code has already distinguished them, and a model that collapses them throws away the only information the reader needs to fix the reference.

## Why the rule exists at all

A reference that silently resolves to the wrong turn is worse than one that fails, because the answer looks like it was grounded. Asking for the reference again costs one message. Quoting the wrong turn costs the reader's trust in every quotation after it.
