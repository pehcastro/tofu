---
topic: tools-code
title: Code intelligence tools
summary: what symbols, map and api give the model: where a name is declared and who calls it, what a change reaches, what matters for a task, and a dependency at its pinned version
verbs: tool
---

## What it is

These tools answer questions about code rather than text, in Go, Rust,
TypeScript and Python.

- `symbols` says where a name is declared and every place that calls it.
  Go is resolved through `go/types`; the rest through `rust-analyzer`,
  `typescript-language-server` and `pyright-langserver`, which follow an
  import alias.
- `symbols` with mode `impact` lists every caller of one declaration by
  kind, `call`, `impl`, `embed`, `test` or `ref`, the tests that reach it
  and the packages.
- `symbols` with mode `rename` and `to` previews a diff per file and the
  errors the rename would add, and writes only with `apply` true. A clash
  with a name already declared in that scope is refused and nothing is
  written.
- `map` ranks the files that matter for a `task`, each with its top
  declarations and their line numbers, in a token `budget`, 1,000 when
  left out, and says what did not fit. `search` orders its files by the
  same rank and says how many matching lines the page cut.
- `api` reads a dependency's signature and doc at the pinned version, from
  the module cache, `node_modules`, the cargo registry or `site-packages`,
  never the network. A package the project does not hold is named with the
  command that adds it.

```text
rename total to amount, resolved by rust-analyzer: 2 sites in 2 files
--- src/lib.rs
+++ src/lib.rs
@@ -9,7 +9,7 @@
 
 impl Order {
     pub fn total(&self) -> u32 {
-        pr::total(self.quantity, self.unit_cents)
+        pr::amount(self.quantity, self.unit_cents)
     }
 
     pub fn discounted(&self) -> u32 {
--- src/pricing.rs
+++ src/pricing.rs
@@ -1,3 +1,3 @@
-pub fn total(quantity: u32, unit_cents: u32) -> u32 {
+pub fn amount(quantity: u32, unit_cents: u32) -> u32 {
     quantity * unit_cents
 }
new errors: none
preview: nothing was written. call again with apply true to write it
```

## Where it lives

The language servers are the project's own, found on `PATH`. tofu starts
a server when a call needs it and stops it after 5 minutes idle or when
tofu exits, and never installs one.

## Change it

Install the server for a language to get answers that follow an import
alias. Without it the answer is matched on the name alone and says so,
naming the install command, such as `rustup component add rust-analyzer`.

## Check it

    tofu tool symbols '{"name":"total"}' --dir .
    tofu tool symbols '{"name":"Order","mode":"impact"}' --dir .
    tofu tool symbols '{"name":"total","path":"src/pricing.rs","mode":"rename","to":"amount"}' --dir .
    tofu tool map '{"task":"Discounted returns 650, want 675"}' --dir .
    tofu tool api '{"package":"github.com/pelletier/go-toml/v2","name":"NewDecoder"}' --dir .

A rename without `apply` writes nothing.

## Undo it

A rename the model applied in a turn is part of that turn, so `/undo`
puts every file it changed back.
