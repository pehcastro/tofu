---
title: Web
description: fetch and web_search are tofu's own HTTP client, and github_pr_diff wraps gh.
order: 5
updated: 2026-10-04
---

`fetch` and `web_search` are tofu's own Go HTTP client. `github_pr_diff`
wraps `gh pr diff`. Every result is wrapped as untrusted: text somebody else
wrote, for the model to read and report, never to follow.

## What fetch keeps and drops

A page as HTML is mostly markup. `fetch` returns text units instead:
headings, paragraphs, list items, table rows and code blocks, code kept
character for character, and drops navigation, forms, scripts and link-only
rows, which are 11.7% of the extracted bytes over 24 recorded pages. A body
over 5 MB is refused.

A long page comes back 300 lines at a time, with its line count and the
`offset` to read on, and the next window is read from the page already
fetched. On a page the size of the recorded average, the model reads 3,590
bytes where it read 21,430.

`web_search` is registered only when its provider is set up, so the model
never sees a tool that would fail. `github_pr_diff` goes through `gh`, which
already holds your login, so tofu never holds a GitHub token.

## Setting up search

- **Search.** Brave is the default and needs a key named `BRAVE_SEARCH_KEY`,
  from tofu's stored keys, the environment, or a `.env` file in the working
  directory. SearXNG needs no key: set its `endpoint` and `use: default` in
  a project file.
- **Override.** A file of the same name in `~/.tofu/web/` or the project's
  `.tofu/web/` replaces the built-in `fetch.yaml` or `search/<provider>.yaml`.
  `use: off` in `fetch.yaml` removes `fetch`.

| Tool | Parameter | What it does |
|---|---|---|
| `web_search` | `query`, `count` | ranked results, 10 at most |
| `fetch` | `url`, `offset`, `limit` | one page as text units, 300 lines at a time by default |
| `github_pr_diff` | `pr` | a number or URL, or the current branch's pull request |

`tofu library` lists the layers a web file is read from:

```text
layers
  library   library
  catalog   ~/.tofu/catalog
  global    ~/.tofu
  project   <project>\.tofu
```
