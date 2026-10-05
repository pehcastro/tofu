---
title: Privacy and safety
description: What tofu sends where, what stays on your machine, how keys stay out of records, and what the browser extension can touch.
order: 5
updated: 2026-10-04
---

tofu keeps every record on your machine, under `~/.tofu`: credentials in
`agent.db`, sessions and the decision ledger under `projects/<project>/`. It
has no telemetry. Text leaves only for the service doing the work:

- the model's provider: Anthropic for `claude-sub/...`, OpenAI for
  `codex-sub/...`, Meta for `meta/...`
- the classifier provider, OpenRouter or TypeSafe: the state of one decision,
  such as a tool call and its input
- Brave Search for `web_search`, and the page `fetch` reaches
- `models.dev` on a model reload, and each subscription for its quota

## Keys stay out of records

A model sees whatever its tools print, so tofu redacts keys before anything
is recorded or sent: every stored key, and any value of `OPENROUTER_KEY`,
`TYPESAFE_API_KEY` or `BRAVE_SEARCH_KEY`, reads `[key redacted]` in tool
results, sessions and the ledger, even after `cat .env`. Other secrets are not
scanned for.

## The browser is fenced

The browser runs in your own Chrome with your logins, so it is fenced: it
never reaches `chrome://`, DevTools, extension pages or the Web Store, drives
only tabs it opened by default, closes them when the run ends, and reads page
text as data, never as an instruction. After 24 page checks it had left 0
files, 0 processes and 0 tabs behind.

## Tightening it

- Make a risky tool call wait for you: in `/settings`, **Interaction**, set
  `gatePrompt` to `ask`. An ask then waits, a deny is refused, and a call the
  classifier cannot judge is refused.
- Limit the browser: in `/settings`, **Browser**, set `browser` to `read` or
  `off`.
- Delete a session: remove its folder under
  `~/.tofu/projects/<project>/sessions/`. tofu never deletes one itself.

> [!WARNING]
> `meta/...-contributor` models cost less because Meta may train on what you
> send. The model list marks each one with `⚠`.

## Commands

```
tofu settings set gatePrompt ask
tofu settings set browser read
tofu login --status --redact
```
