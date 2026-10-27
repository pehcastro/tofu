---
title: API keys
description: The keys you can bring to tofu, what each one is for, where tofu finds them, and how they stay hidden.
order: 4
updated: 2026-10-22
---

A key is a credential you bring from a provider, billed by that provider.
Each key is stored under the model type it pays for, so one provider can
hold two keys that never mix:

| Role | Key | Variable | Job |
|---|---|---|---|
| `llm` | `meta` | `META_MUSE_API_KEY` | Meta's models, `meta/...`, for the lead or a sub-agent |
| `llm` | `openrouter` | `OPENROUTER_LLM_KEY` | a chat model on OpenRouter, for the lead or a sub-agent |
| `classifier` | `typesafe` | `TYPESAFE_API_KEY` | the classifier, Jev, through TypeSafe |
| `classifier` | `openrouter` | `OPENROUTER_KEY` | the classifier, Jev, through OpenRouter |
| `search` | `brave` | `BRAVE_SEARCH_KEY` | the `web_search` tool |

A key decides who pays, never which model runs: the model each role runs is
the one you bind with `tofu models bind`, see [Overview](/docs/llms/overview).

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
paste the TypeSafe key, then press enter:
› tsk-…3f9a  51 chars  prefix not checked
```

A pasted line break is dropped. A key that does not start with the
provider's prefix is named, and enter must be pressed a second time to keep
it, so a vendor that changes its prefix never locks a valid key out. Only
OpenRouter's prefix is checked today; the others say `prefix not checked`.
Piped in, tofu prints the same line once. In Git Bash or mintty, where the
terminal shows what you type, tofu warns before reading.

## Storing, replacing and removing a key

- Store a key: `tofu login <role> <name>`, or pipe it in:
  `tofu login classifier typesafe < key.txt`. A classifier or Meta key is
  checked before it is written.
- Replace it: run the same `tofu login` again.
- Remove a stored key: `tofu logout <role> <name>`. When the same key is
  also set in the environment or an `.env`, tofu says so, because that one
  is still read.
- Use the environment instead: the variable in the table above, or that
  line in an `.env` file in the working directory. The credential store is
  read first.

## What a key has spent

`tofu login --status` shows, under each stored key, every call tofu made
on it, so you see what a key costs without opening the provider's site.
Every call on a key is one line in `~/.tofu/keyuse/<date>.jsonl`, written
by every tofu process, with the variable the key came from and never the
key itself.

A stored key is a card of at most three lines, and a key not stored is one
line with the command that stores it. No line wraps; a longer one is cut
with `…`.

| Row | What it says |
|---|---|
| `use` | what the key serves |
| `calls` | how many calls, when the key was last used, and their cost in all, `est.` when priced from the list |
| `limit` | the spend limit and what is left, or `no cap` and this month's spend, the account balance, and a rate limit read from the last call |
| `error` | in place of `limit`, a refusal newer than the last good call, such as `402 account out of credit · 3m ago` |

`tofu login --status --json` carries every figure behind the card: calls
and tokens for today, 7 days, 30 days and in all, per model, the model ids
the provider said it ran, and what each provider reports.

Every cost says where it came from: `reported by the provider`,
`at list price, estimated` when the provider gives no cost, or `no price`.
On OpenRouter a call also shows what the model's own provider charged, as
`charged upstream`. The key limit and the balance are read from
OpenRouter's `/api/v1/key` and `/api/v1/credits` each time you ask; a key
that may not read the balance says it needs a management key.

| Provider | Reports | Does not report |
|---|---|---|
| Meta | tokens per call, team rate limits | spend or balance, so cost is estimated from list price |
| OpenRouter | spend and limits for the key, a cost per call, the account balance | per-model history without a management key |
| TypeSafe | tokens per call | spend, balance or limits, so cost is estimated from list price |
| Brave | monthly calls remaining and the reset | spend, so cost is estimated from 5 USD per 1000 calls |

## Commands

```
tofu login llm <meta|openrouter>
tofu login classifier <typesafe|openrouter>
tofu login search brave
tofu logout <llm|classifier|search> <name>
tofu login --status
tofu models bind classifier typesafe/jev-latest
tofu run --dir . --model meta/muse-spark-1.3 "<task>"
```

`tofu login --status` lists each key under its role, here with made-up
figures:

```
language model
  ┌ OpenRouter  ····a1b2 ──────────────────────────────────── ✓ set
  │ use       serves the openrouter models on your own key
  │ calls     12 calls · last 3m ago · 0.0412 USD
  │ limit     8.50 USD of 10.00 USD left · balance 4.20 USD
  └─────────────────────────────────────────────────────────────────

  ○ Meta  not set  → tofu login llm meta

classifier · jev, required
  ○ TypeSafe    not set  → tofu login classifier typesafe
  ○ OpenRouter  not set  → tofu login classifier openrouter
```
