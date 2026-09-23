---
id: quote-resolution
domain: general
document: interface/tui/quote and internal/session, read in this repository on 2026-09-22
found: this repository; this file is a description of what the code does, not a quotation
---

# How a quote reference resolves

A message can carry `[quote#abcd]`, where `abcd` is a short prefix of a recorded event id. Two functions turn that into a turn, and between them they already name every outcome the rule has to talk about.

## What the two functions do

`interface/tui/quote.Collect` walks a conversation and keeps one `Turn` per utterance: the event id, who said it, and the first non empty line of what was said, or the names of the tools that ran when there was no text. A turn recorded before event ids existed is not skipped. It is given an id derived from the session and the turn's place in it, by `internal/session.EventIDFor`, so an old transcript is quotable on the same terms as a new one.

`interface/tui/quote.Resolve` hands those ids to `internal/session.FindByHash` and returns the turn behind the one that matched.

## The three outcomes are the function's own

`FindByHash` counts how many recorded ids the short reference is drawn from and returns one of three things:

- exactly one match: that event
- no match: `ErrEventHashNotFound`, naming the reference
- more than one: `ErrEventHashAmbiguous`, naming the reference and how many events it matched

Nothing falls back to the nearest turn, and there is no fourth case. That is why the rule says an id matching no turn and an id matching more than one resolve to neither, and why it says to name which of the two happened rather than answer anyway: the code has already distinguished them, and a model that collapses them throws away the only information the reader needs to fix the reference.

## Why the rule exists at all

A reference that silently resolves to the wrong turn is worse than one that fails, because the answer looks like it was grounded. Asking for the reference again costs one message. Quoting the wrong turn costs the reader's trust in every quotation after it.
