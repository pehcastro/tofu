---
title: API keys
description: The keys you can bring to tofu, what each one is for, where tofu finds them, and how they stay hidden.
order: 4
updated: 2026-10-05
---

A key is a credential you bring from a provider, billed by that provider.
tofu takes four, each for one job, and each is stored under the role it
serves:

| Role | Key | Job |
|---|---|---|
| `llm` | `meta` | Meta's models, `meta/...`, for the lead or a sub-agent |
| `classifier` | `openrouter` | the classifier, through OpenRouter |
| `classifier` | `typesafe` | the classifier, through TypeSafe |
| `search` | `brave` | the `web_search` tool |

## One job per key

A key spends money, so each one is scoped to a single job and the model list
says which models it pays for. The classifier makes many small calls, so a
cheap key fits it: over 89 tool calls Jev, through the classifier key, spent 0.004290 USD in
total, where Opus on a key spent 0.769200 USD on the same calls. Keys stay out
of your history and your records: tofu never takes a key as an argument,
prints at most its last four characters once it is stored, and writes
`[key redacted]` wherever its value would appear.

## You can see the paste worked

While you type or paste a key, the field shows its shape and never the key:
the provider's public prefix, or its first four characters when the prefix
is not known, then its last four, its length, and whether the prefix is the
one that provider uses.

```
paste the OpenRouter key, then press enter:
› sk-or-v1-…3f9a  73 chars  prefix ok
```

A pasted line break is dropped. A key that does not start with the
provider's prefix is named, and enter must be pressed a second time to keep
it, so a vendor that changes its prefix never locks a valid key out. Only
OpenRouter's prefix is checked today; the others say `prefix not checked`.
Piped in, tofu prints the same line once. In Git Bash or mintty, where the
terminal shows what you type, tofu warns before reading.

## Storing, replacing and removing a key

- Store a key: `tofu login <role> <name>`, or pipe it in:
  `tofu login classifier openrouter < key.txt`. A classifier or Meta key is
  checked before it is written.
- Replace it: run the same `tofu login` again.
- Remove a stored key: `tofu logout <role> <name>`. When the same key is
  also set in the environment or an `.env`, tofu says so, because that one
  is still read.
- Use the environment instead: `OPENROUTER_KEY`, `TYPESAFE_API_KEY` or
  `BRAVE_SEARCH_KEY`, or that line in an `.env` file in the working
  directory. The credential store is read first.

## Commands

```
tofu login llm meta
tofu login classifier <openrouter|typesafe>
tofu login search brave
tofu logout <llm|classifier|search> <name>
tofu login --status
tofu run --dir . --model meta/muse-spark-1.3 "<task>"
```
