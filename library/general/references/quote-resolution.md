---
id: quote-resolution
domain: general
document: interface/tui/quote, internal/turn/tools and internal/session, read in this repository
also: library/general/references/what-reaches-a-screen.md
---

# How a quote reference resolves

A message can carry `[quote#abcd]`, where `abcd` is the tail of a recorded event id. The `quote` tool turns that into the recorded item it names, and the function under it already names every outcome the rule has to talk about.

## What an item is

An item is anything the screen shows with an id:

- a turn: what the person said, what the agent said, or what a tool returned
- a tool call: an edit, a shell command, a question asked of the person, a sub-agent spawned, or a call a sub-agent made

The token is the same for every kind. `internal/turn/tools.QuoteRef` builds it, `[quote#` and the last six characters of the id, and the interface and the server both call it, so a token typed by hand, inserted by ctrl+r or sent over the wire is one string.

## What the tool does

`internal/turn/tools.Quote` takes the reference as the model received it, reads the recorded session it belongs to and every session that session was forked from, and returns the item.

- A turn comes back as its own words and the names of the tools that ran, never their arguments. What the person typed comes back as it was recorded, which is the text that was already sent when the turn happened.
- A tool call comes back as its arguments and what it returned, with every key tofu knows masked. A call still running says no result is recorded for it yet.

An item larger than the tool result cap comes back cut at the cap, with a note naming its whole size in bytes and how many of them are there. The session record on disk is never written by a quote.

## The three outcomes

`internal/turn/tools.ResolveQuote` counts how many recorded ids the short reference is drawn from, turns and tool calls together, and returns one of three outcomes:

- `item`: exactly one match, and that item
- `not_found`: no match in the session or any it was forked from
- `ambiguous`: more than one match in one session, a turn and a call included

Nothing falls back to the nearest item, and there is no fourth case. That is why the rule says an id matching no item and an id matching more than one resolve to neither, and why it says to name which of the two happened rather than answer anyway: the code has already distinguished them, and a model that collapses them throws away the only information the reader needs to fix the reference.

A turn recorded before event ids existed is not skipped. `internal/turn/tools.SaidEventID` gives it an id derived from the session and the turn's place in it, and `interface/tui/quote.Collect` uses the same function, so an old transcript is quotable on the same terms as a new one.

## Why the rule exists at all

A reference that silently resolves to the wrong item is worse than one that fails, because the answer looks like it was grounded. Asking for the reference again costs one message. Quoting the wrong item costs the reader's trust in every quotation after it.
