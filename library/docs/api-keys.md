---
topic: api-keys
title: What an API key has spent
summary: tofu login --status shows each API key's calls, tokens and cost with where the cost came from, the limits and balance the provider reports, and what that provider does not report
---

## What it is

`tofu login --status` lists each key under its role: language model,
classifier and search. A stored key shows `✓` and its last four
characters; one not stored shows `○ not set` and the command that stores
it. Under a stored key, tofu shows what it recorded and what the provider
said:

- `calls`: how many calls tofu made on the key and when it last used it;
  `no calls yet` before the first
- `today`, `7d`, `30d` and `all`: calls, input, output, cache read, cache
  write and reasoning tokens, and the cost. Periods with the same figures
  fold into one row, such as `today to all`
- `on <model>`: the same figures per model, when more than one answered
- `answered by`: the model ids the provider said it ran
- `key limit`: the key's spend limit and what is left, and what the
  provider says the key spent today, this week, this month and in all
- `balance`: the account's credit, bought and used
- `allowance`: a rate limit read from the last call, in calls or tokens,
  per key or per team, with when it resets
- `last error`: the newest refusal, such as `402 account out of credit`
- `reports`: what this provider reports and what it does not

Every cost says where it came from:

- `reported by the provider`: the provider put the cost on the call
- `at list price, estimated`: tofu priced the tokens from the model's list
  price, because the provider gives no cost
- `no price`: neither is known
- `charged upstream`: on OpenRouter, what the model's own provider charged
  for the call, beside OpenRouter's cost

What each provider gives:

- Meta: tokens per call and team rate limits; no spend or balance, so cost
  is estimated from list price
- OpenRouter: spend and limits for the key, a cost per call, and the
  account balance; per-model history needs a management key
- TypeSafe: tokens per call; no spend, balance or limit, so cost is
  estimated from list price
- Brave: monthly calls remaining and the reset; cost is estimated from
  5 USD per 1000 calls

## Where it lives

`~/.tofu/keyuse/<date>.jsonl` holds one line per call on a key: the
variable the key came from, never the key, the provider, the model, the
tokens, the cost and its source, the allowances the reply carried, and
any refusal. Every tofu process writes there, so the totals cover every
session and every project.

The key limit and the balance are asked of OpenRouter each time the
status is printed, from `/api/v1/key` and `/api/v1/credits`, with 5 s
for each. A key that may not read the balance says
`the account balance needs a management key`.

## Change it

There is nothing to set. Store, replace or remove a key with
`tofu login <role> <name>` and `tofu logout <role> <name>`; the figures
follow the key's variable, so a replaced key keeps the history.

## Check it

    tofu login --status

A stored OpenRouter key reads like this, with made-up figures:

    ✓ OpenRouter  ····a1b2  serves the openrouter models on your own key
        calls         12 calls · last used 3m ago
        today to all  12 calls · 48210 in, 1904 out tokens · 0.0412 USD
          reported by the provider
        answered by   deepseek/deepseek-v3
        key limit     8.50 USD of 10.00 USD left · spent today 0.04 USD,
          week 0.90 USD, month 1.50 USD, all 1.50 USD
        balance       4.20 USD (20.00 USD bought, 15.80 USD used)

`tofu login --status --json` carries the same under each key, and
`query.accounts` over `tofu serve` answers it.

## Undo it

Delete a day's file under `~/.tofu/keyuse/` to forget its calls, or the
folder to forget them all; the next call starts a new one. A key limit or
a balance is never stored, so there is nothing to clear.
