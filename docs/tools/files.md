---
title: Files
description: read, write, edit, glob, search, symbols and project_report, tofu's own Go tools for finding, reading and changing files.
order: 2
updated: 2026-11-06
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

`read` is `os.ReadFile`, whole, from `start_line` to `end_line`, or about
100 lines around `at`.

A range gets one header, `path lines a-b of N`, and no line carries
a number, so no bytes go to numbering. A path that doesn't exist is repaired
when exactly one file has that name, and the repair is named first.

`at` is a line number or a name the file declares, such as `Total` or
`Order.Total`, in a `.go`, `.rs`, `.ts` or `.tsx` file. The header states
the range read and where the declaration sits:

```bash
tofu tool read '{"path":"order.go","at":"Total"}' --dir .
```

```text
order.go lines 1-16 of 16, method Order.Total at lines 10-12
```

A name the file doesn't declare is refused with the closest names it does:

```text
error: read: order.go declares no Totl: the closest names it declares are Order.Total, Order, Order.Discounted
```

A second read of the same lines of a file that hasn't changed since the
agent's last read of them comes back as one line, whether a call such as
`bash` ran between the two reads or nothing did:

```text
order.go: unchanged since your read at step 1, whose result is still above
```

The earlier result is still in the conversation, so the model reads it
there. Once the file changes, or `forget` or a compaction drops the earlier
result, the next read is whole again.

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
| `at` | string | a line number or a declared name to read around |

## write

`write` creates a file or replaces one whole, and answers with a
unified diff.

Replacing a file the session hasn't read, or one that changed since,
is refused, and the refusal carries the current content, so the model writes
over what's there rather than a guess. A `.ts` or `.tsx` file is then
typechecked with the project's `tsc`, a `.go` file with `go vet`, a `.rs`
file with `cargo check` and a `.py` file with `pyright` or `mypy`, and their
errors end the result.

| Parameter | Type | What it does |
|---|---|---|
| `path` | string | the file |
| `content` | string | the whole new content |

## edit

`edit` replaces one exact stretch of a file, or one whole Go
declaration by name, and answers with the diff.

`old_string` must appear exactly once. When it appears more often,
the result lists each occurrence with its line, instead of changing the first.
With `replace_all` every exact occurrence is replaced and the result says how
many, which is how a name is renamed through a file without `sed -i`.
A whitespace-only mismatch is repaired when there's one candidate. An empty
`old_string` on an existing file appends `new_string` to its end.

An edit that would leave a Go file unable to parse is refused with the
parser's line, and the file stays as it was:

```bash
tofu tool edit '{"path":"order.go","old_string":"return o.Total() - 100\n}","new_string":"return o.Total() - 100"}' --dir .
```

```text
error: edit: this edit would leave order.go unable to parse as go: order.go:15:25: expected '}', found 'EOF'. nothing was written, and order.go parses as it stands
```

An edit that parses ends with the errors the file's checker finds, the same
checkers `write` uses, so a type error comes back with the change that made
it:

```text
typecheck: go vet ./. in 329 ms: 1 error
./order.go:15:9: invalid operation: o.Total() - "100" (mismatched types int and untyped string)
```

One call can change several files with `edits`, a list of changes each with
its own `path`, the way codex's patch and omp's edit do. They are all written
or none is: every safety check reads every file in the call, and the result
names the file that failed and gives a diff and a check per file.

| Parameter | Type | What it does |
|---|---|---|
| `path` | string | the file |
| `old_string` | string | the exact text to replace |
| `symbol` | string | or a Go declaration, such as `Resolve` or `Root.Resolve` |
| `new_string` | string | what it becomes; empty deletes |
| `replace_all` | boolean | replace every occurrence of `old_string`, not one |
| `edits` | list | changes for several files at once, each with `path`, `old_string`, `new_string` |

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

`symbols` answers where an identifier is declared, what it calls, and who
calls it, in Go, Rust and TypeScript (`.go`, `.rs`, `.ts`, `.tsx`): Go
through the Go parser, Rust and TypeScript by their braces. The owns check
on `tofu boardy move <id> doing` reads the same three. It matches on the
name rather than types, so
a call through an interface isn't found, and the result says so. Parameters:
`name`, `path`.

## project_report

`project_report` is one walk that returns the file count and size, languages, top
level directories, entry points, manifests and docs, 12 lines a section. The
model calls it first in a project it doesn't know. Parameter: `limit`.

## Read before edit

The model chooses these tools. You decide one thing: `readBeforeEdit`, on by
default, in settings. With it on, both refuse a file changed since it was
read, and `write` refuses replacing a file never read. `edit` refuses a file
never read only when it is too large to show whole: an exact edit of a small
one is applied, since the refusal would only print the file and let the same
edit through. An exact edit whose text appears once in a file unchanged since
it was read is applied even outside the lines read; text that appears twice
there, or a file changed since, is refused.

```bash
tofu settings get readBeforeEdit
```

```text
true
```
