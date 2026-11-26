---
topic: tools-change
title: The tools that change code
summary: what codemod, scaffold, format and deps give the model: one code shape rewritten everywhere on the syntax tree, a new file shaped like a sibling, the project's formatter on the files named, and a dependency added after its license, size and advisories are read
verbs: tool
---

## What it is

These tools change code the way a person means it rather than the way
the text looks.

- `codemod` rewrites one code shape everywhere, matched on the syntax
  tree, so a string or comment that looks the same is never touched. It
  runs on cat-grep, a Go port of ast-grep that parses with tree-sitter
  grammars built into the binary: 47 languages with nothing to install,
  the script code of Svelte, Vue and PHP pages included. `pattern` and
  `rewrite` are code in `lang`, in ast-grep's pattern language: `$NAME` matches one node,
  `$$$NAME` a run of nodes. The first call previews every site and the
  diff; `apply` true writes them, refuses when `expect` differs from the
  count, and ends with the checker's errors. A Rust rename also renames
  the name inside a format string, and `cargo check --all-targets` after
  it checks the test code.
- `codemod` with `rule` in place of `pattern` matches by `kind`, `regex`,
  `inside`, `has`, `not`, `all` and `any`, such as a name only inside one
  function.
- `scaffold` copies the file named in `like` beside it, with the sibling's
  name replaced by `name` in every case form, in the contents and the
  file name. A target that already exists is refused.
- `format` runs gofmt, rustfmt, prettier or ruff over the files in
  `paths`, or the files this turn changed, and nothing else. A missing
  formatter is named with how to install it.
- `deps` adds and upgrades Go, npm, cargo and uv dependencies after
  reading the license from deps.dev and the advisories from osv.dev,
  refuses a version with an advisory unless `accept_advisories` is true,
  and answers `why` and `outdated`.

Renaming a local while keeping the message text, in Go, TypeScript,
Python and Rust, `codemod` changed 0 sites wrong; `edit` with `replace_all`
changed 2 in each.

    codemod: 6 matches in 1 file. applied
    src/lib.rs:26 got -> actual
    src/lib.rs:28 got -> actual
    ...
    -        assert_eq!(got, want, "got {got}, want {want}");
    +        assert_eq!(actual, want, "got {actual}, want {want}");
    ...
    checker: typecheck: cargo check --all-targets in 278 ms: 0 errors

## Where it lives

cat-grep and gofmt are inside tofu. rustfmt, prettier and ruff are the
project's own, from `node_modules`, the project's `.venv` or `PATH`;
tofu never installs one. `deps` reads the registries, deps.dev and
osv.dev over the network and runs the project's own package manager.

## Change it

Install a formatter to let `format` run it, such as `npm install
--save-dev prettier` or `pip install ruff`.

## Check it

    tofu tool codemod '{"lang":"go","pattern":"t.Fatalf($$$ARGS)","rewrite":"t.Errorf($$$ARGS)"}' --dir .
    tofu tool scaffold '{"kind":"type","name":"Invoice","like":"order.go"}' --dir .
    tofu tool format '{"paths":["label.go"]}' --dir .
    tofu tool deps '{"mode":"outdated"}' --dir .

A `codemod` without `apply` writes nothing.

## Undo it

A change these tools made in a turn is part of that turn, so `/undo`
puts every file it changed back.
