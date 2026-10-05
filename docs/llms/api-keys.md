---
title: API keys
description: The keys you can bring to tofu, what each one is for, where tofu finds them, and how they stay hidden.
order: 4
updated: 2026-10-04
---

A key is a credential you bring from a provider, billed by that provider.
tofu takes four, each for one job:

| Key | Job |
|---|---|
| `meta` | Meta's models, `meta/...`, for the lead or a sub-agent |
| `openrouter` | the classifier, through OpenRouter |
| `typesafe` | the classifier, through TypeSafe |
| `brave` | the `web_search` tool |

## One job per key

A key spends money, so each one is scoped to a single job and the model list
says which models it pays for. The classifier makes many small calls, so a
cheap key fits it: over 89 tool calls Jev, through the classifier key, spent 0.004290 USD in
total, where Opus on a key spent 0.769200 USD on the same calls. Keys stay out
of your history and your records: tofu asks for a key unechoed, never as an
argument, prints at most its last four characters, and writes
`[key redacted]` wherever its value would appear.

## Storing, replacing and removing a key

- Store a key: `tofu login <name>`, or pipe it in:
  `tofu login openrouter < key.txt`. A classifier or Meta key is checked
  before it is written.
- Replace it: run `tofu login <name>` again.
- Use the environment instead: `OPENROUTER_KEY`, `TYPESAFE_API_KEY` or
  `BRAVE_SEARCH_KEY`, or that line in an `.env` file in the working
  directory. The credential store is read first.
- Remove one from the environment or `.env` by deleting its line. No verb
  removes a stored key yet.

## Commands

```
tofu login <openrouter|typesafe|meta|brave>
tofu login --status
tofu run --dir . --model meta/muse-spark-1.3 "<task>"
```
