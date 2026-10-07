---
title: Files
description: read, write, edit, glob, search, symbols and project_report, tofu's own Go tools for finding, reading and changing files.
order: 2
updated: 2026-10-07
---

Seven tools, all tofu's own Go code, none of them a wrapper around `cat`,
`sed`, `find` or `grep`. They share one root, the working directory, refuse a
path that leaves it, and skip what `.gitignore` skips. A read is recorded in
the session's read ledger, which `write` and `edit` check before they change
anything.

## Why not the shell

The shell way works, and fails quietly. On 40 real edits from recorded runs,
`sed` on the changed line was silently wrong 12 times, including every
near-duplicate line, and changed the wrong match of an ambiguous one 4 times.
`edit` was right or refused with a reason all 40 times. On 40 ranged reads,
`cat` returned 785 KB where `read` returned 58 KB, and needed a second call 11
times. Listings are the largest thing a model reads: capping `glob` at 300
paths cut 88.4% of all tool output bytes over 1,401 calls.

## read

`read` is `os.ReadFile`, whole or from `start_line` to `end_line`.

A range gets one header, `path lines a-b of N`, and no line carries
a number, so no bytes go to numbering. A path that doesn't exist is repaired
when exactly one file has that name, and the repair is named first.

A png, jpeg, gif or webp comes back as a picture the model can see, with one
line of text naming it, such as `shapes.png: png, 400x300, 2753 bytes, attached
as a picture`. The file's bytes decide, not its name, so a text file called
`notes.png` still reads as text. A picture past 1568 pixels on its long edge or
1.15 megapixels is shrunk to fit first, because Anthropic shrinks it to that
size anyway and charges about width x height / 750 tokens, so a fitted picture
costs at most about 1,600. The text line names both sizes. A webp past 5 MB is
refused, since tofu cannot shrink one. The session keeps that text line and not
the picture, so a resumed session knows which file was looked at and reads it
again to see it.

| Parameter | Type | What it does |
|---|---|---|
| `path` | string | the file, relative to the working directory |
| `start_line`, `end_line` | integer | the range, counted from 1, both included |

## write

`write` creates a file or replaces one whole, and answers with a
unified diff.

Replacing a file the session hasn't read, or one that changed since,
is refused, and the refusal carries the current content, so the model writes
over what's there rather than a guess. A `.ts` or `.tsx` file is then
typechecked and its errors end the result.

| Parameter | Type | What it does |
|---|---|---|
| `path` | string | the file |
| `content` | string | the whole new content |

## edit

`edit` replaces one exact stretch of a file, or one whole Go
declaration by name, and answers with the diff.

`old_string` must appear exactly once. When it appears more often,
the result lists each occurrence with its line, instead of changing the first.
A whitespace-only mismatch is repaired when there's one candidate, and an
edit that would leave a Go file unable to parse is refused with nothing
written.

| Parameter | Type | What it does |
|---|---|---|
| `path` | string | the file |
| `old_string` | string | the exact text to replace |
| `symbol` | string | or a Go declaration, such as `Resolve` or `Root.Resolve` |
| `new_string` | string | what it becomes; empty deletes |

## glob

`glob` is a `filepath.WalkDir` with tofu's own `.gitignore` parser,
returning at most 300 paths. Patterns are written the way other tools take
them: `src/**/*.ts` reaches any depth, `*.{ts,tsx}` matches either
extension, and a pattern with an unclosed brace is refused with where it
broke.

`find` and `ls -R` enter every ignored directory. On 20 listings,
`glob` returned 21 KB where `ls -R` returned 126 KB.

| Parameter | Type | What it does |
|---|---|---|
| `pattern` | string | matched against the whole path and the file name, so `*.ts` finds every depth |
| `path` | string | where to start |
| `include_ignored` | boolean | walk ignored paths too |

## search

`search` runs Go's `regexp` over every text file and returns the whole
declaration around each match within a 4,000-token budget. There is no `grep`
tool.

A match in a `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.py` or `.rs` file comes
back as the entire function, method, class or type, marked as code, comment
or string, so the model rarely needs a `read` after it. Over 30 searches per
corpus, that cut what the model read from 87k tokens to about 40k in Python,
from 154k to 52k in TypeScript, and from 83k to 52k in Rust. A file in any
other language comes back as the lines around the match. When nothing
matches, a case-insensitive retry tells an absence from a wrong pattern.

| Parameter | Type | What it does |
|---|---|---|
| `pattern` | string | a Go regular expression |
| `path` | string | narrow the search |
| `max_tokens` | integer | raise the budget |

## symbols

`symbols` uses the Go parser to answer where an identifier is declared,
what it calls, and who calls it. It matches on the name rather than types, so
a call through an interface isn't found, and the result says so. Parameters:
`name`, `path`.

## project_report

`project_report` is one walk that returns the file count and size, languages, top
level directories, entry points, manifests and docs, 12 lines a section. The
model calls it first in a project it doesn't know. Parameter: `limit`.

## Read before edit

The model chooses these tools. You decide one thing: `readBeforeEdit`, on by
default, in settings.

```bash
tofu settings get readBeforeEdit
```

```text
true
```
