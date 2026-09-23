---
id: what-reaches-a-screen
domain: general
document: internal/turn, cmd/tofu and internal/secret, read in this repository on 2026-09-23
found: this repository; this file is a description of what the code does, not a quotation
---

# What reaches a screen

A tool's `Result.Command` label is published on purpose and is drawn whole. A call's arguments JSON is never drawn, and a running child carries tool names only.

## The three decisions behind it

They were settled one at a time and they are one rule.

The quote tool hands back what was said and the names of the tools that ran. A call's arguments and a tool's output never come back, so a key pasted into a command line is not repeated by a quotation.

A running child carries tool names only. The watcher copies the name of the tool being called and reaches no other field, so a child mid-call shows `bash` and nothing more.

A finished child's call text is the tool's own label. `Result.Command` is a one line description each tool writes for itself: `read a.txt`, `write hello.txt`, `edit` followed by the path it changed. For `bash` that label is the command line, and that is the point. A person watching a child wants to know it ran the tests for one package, not that it ran `bash`.

So the one string on a screen that can carry what a person typed is a bash label, and it is there because a reader needs it.

## Scrub does not redact

`internal/secret.Scrub` replaces a marker prefix and keeps every value byte behind it. An Anthropic OAuth token prefix becomes the words `redacted-anthropic-oauth-token` and the token itself is untouched, twenty bytes longer and just as readable.

It is a fixture anonymiser. It exists so a recorded session can be checked in, by turning one machine's home directory and one person's name into neutral ones. Anyone reaching for it to keep a key off a screen gets nothing.

`LeaksIn` and `CredentialsIn` are the detectors. They say which marker a text contains and never change the text, and they are what the tickets that proved something about credentials actually used.

## The measured rate, so nobody over-corrects

110 recorded turns were walked for credential markers. Two matched. Both were a person writing a marker's name inside a sentence, and neither was a credential. Zero real credentials.

Of the 231 bash labels in those sessions, none carries a credential marker. Two would be rewritten by `Scrub`, both on a name and a path, and both rewrites turn a path a reader can copy into one that does not exist.

That is the shape of it. A hole nobody has fallen into, and a scrubber that would mangle a person's own words for a rate of zero.

## What cites this page

`library/general/references/quote-resolution.md` names it as related reading, and the `quote` rule cites that page. No rule names this page directly. The rule that would is not written, and writing one here to give the page a citation would ship a rule nothing measured.
