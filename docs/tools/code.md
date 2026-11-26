---
title: Code intelligence
description: symbols, map, api and eval, the tools that answer where a name is declared and who calls it, what a change reaches, what matters for a task, what a dependency looks like at the pinned version, and what an expression returns.
order: 12
updated: 2026-11-25
---

Four tools answer questions about code rather than text. `symbols` says
where a name is declared and every place that calls it, what else a change
reaches, and renames one declaration everywhere it is used. `map` ranks the
files that matter for a task and fits them in a token budget. `api` reads a
dependency's signature and doc at the version the project pinned. `eval`
returns what one expression evaluates to in the project. All four are
tofu's own Go code and read Go, Rust, TypeScript and Python; `symbols` and
`map` read Zig too.

## Why they exist

A text search finds a name, not a declaration. It misses a call made
through an import alias, such as `pr.total` after `import * as pr`, and it
matches every other declaration that happens to share the name. Renaming by
text then either changes code it should not or leaves a call behind that no
longer builds.

On a rename through an import alias in Go, TypeScript, Rust and Python,
`symbols` missed 0 sites, changed 0 it should not have, and every build
passed. A replace of the bare name changed 14 sites it should not have, and
a replace qualified by the package missed 4 and broke the build in all 4
languages.

## How names are resolved

Go is resolved through `go/types`. Rust, TypeScript and Python are resolved
by the project's own language server, spoken to over the
[Language Server Protocol](https://microsoft.github.io/language-server-protocol/):
`rust-analyzer`, `typescript-language-server` and `pyright-langserver`.
Zig is matched on the name alone, and the answer says so. tofu starts the
server on the first call that needs it, keeps it running for the next
call, and stops it after 5 minutes idle or when tofu exits. tofu never installs a server.
When one is not on `PATH`, the answer is matched on the name alone, says
so, and names the install command:

```text
rust: rust-analyzer is not on PATH, and tofu never installs it: install it with rustup component add rust-analyzer. until it answers, these rust call sites are matched on the name alone and a call through an import alias is missed
```

## symbols

`symbols` takes a `name` and answers where it is declared and every place
that calls it. A method is written `Type.method`. The answer through a
language server follows an import alias: here `pr.total` in `order.ts` is
the caller of `total` in `pricing.ts`.

```bash
tofu tool symbols '{"name":"total"}' --dir .
```

```text
symbols total under .
total in typescript, resolved by typescript-language-server: 2 definitions, 3 callers, 0 other references

defined src/order.ts:12 method Order.total
  called from 2 places
    src/order.test.ts:6 in test("total") callback
    src/order.ts:17 in discounted

defined src/pricing.ts:1 func total
  called from 1 place
    src/order.ts:13 in total
```

### impact

Mode `impact` answers what else must change if one declaration changes:
every caller by kind, `call`, `impl`, `embed`, `test` or `ref`, the tests
that reach it, directly or through other code, and the packages.

```bash
tofu tool symbols '{"name":"Order","mode":"impact"}' --dir .
```

```text
impact of Order, resolved by go/types: 4 callers (2 test, 2 ref), 2 tests reaching it, 1 package
defined order.go:5 type Order

callers
  ref   order.go:10 in Order.Total
  ref   order.go:14 in Order.Discounted
  test  order_test.go:6 in TestTotal
  test  order_test.go:14 in TestDiscounted

tests that reach it
  order_test.go:13 TestDiscounted
  order_test.go:5 TestTotal

packages
  fixture.test/orders
```

`test` with `changed` reads the same answer to pick which tests to run. See
[Checks](/docs/tools/checks#repeat-and-changed).

### rename

Mode `rename` renames exactly one declaration and every site that resolves
to it, import aliases included. It shows a diff per file and the errors the
rename would add, and writes nothing until the call carries `apply` set to
`true`.

```bash
tofu tool symbols '{"name":"total","path":"src/pricing.rs","mode":"rename","to":"amount"}' --dir .
```

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

`Order.total` and the other `total` in `lib.rs` are left alone: they are
different declarations with the same name.

A rename to a name already declared in the same scope, or one that would
add an error, is refused and nothing is written:

```text
error: symbols: rename Order.discounted to total, resolved by rust-analyzer is refused: src/lib.rs:11 already declares total in the same scope as Order.discounted. nothing was written
```

Rename and impact need one declaration. When a name has several, write the
method as `Type.method` or narrow `path` to the file that declares it.

To replace one declaration's body or insert code beside it, `edit` takes the
same names. See [edit](/docs/tools/files#edit).

## map

`map` answers what matters in the project for a task before the model reads
files one by one. It ranks the files by how the code refers to itself, each
with its top declarations as one-line signatures and their line numbers,
and cuts the list to fit a token budget. A word of `task` that is a declared
name or a file or folder name seeds the rank, and `files` seeds it with
paths.

```bash
tofu tool map '{"task":"Discounted returns 650, want 675"}' --dir .
```

```text
map under .: 4 of 4 files, 8 of 8 declarations, 127 of 1000 tokens
seeded by 1 file, through the task's words Discounted

order.go rank 0.702
  5 type Order struct
  10 func (o Order) Total() int
  14 func (o Order) Discounted() int

pricing/pricing.go rank 0.298
  3 func Total(quantity, unitCents int) int

broken/broken.go rank 0.000
  3 func Label() int

order_test.go rank 0.000
  5 func TestTotal(t *testing.T)
  13 func TestDiscounted(t *testing.T)
  21 func TestShipping(t *testing.T)

more: none
```

The budget is 1,000 tokens when left out. A smaller one keeps the highest
ranked files and says what did not fit:

```text
map under .: 1 of 4 files, 2 of 8 declarations, 69 of 70 tokens
seeded by 1 file, through the task's words Discounted

order.go rank 0.702
  10 func (o Order) Total() int
  14 func (o Order) Discounted() int

more: 3 of 4 files and 6 of 8 declarations did not fit the budget
```

The same call on the same files prints the same answer. `map` ranks Go,
Rust, TypeScript, Python and Zig files. `search` orders its results by the
same rank. See
[search](/docs/tools/files#search).

## api

`api` answers what a dependency looks like at the version the project
pinned. It reads from disk and never from the network: the module cache for
Go, `node_modules` for TypeScript, the cargo registry for Rust, and
`site-packages` for Python. The answer names the version, where it was
read, and each declaration's signature and doc.

```bash
tofu tool api '{"package":"github.com/pelletier/go-toml/v2","name":"NewDecoder"}' --dir .
```

```text
package: github.com/pelletier/go-toml/v2
version: v2.2.2
source: module cache, C:\Users\you\go\pkg\mod\github.com\pelletier\go-toml\v2@v2.2.2

declared: unmarshaler.go:44
signature:
func NewDecoder(r io.Reader) *Decoder
doc:
NewDecoder creates a new Decoder that will read from r.
```

The version is the one `go.mod`, `package.json` with its `node_modules`,
`Cargo.lock` or the project's Python holds, even when a newer one sits in
the same cache. A Rust name is written `Error::msg`:

```text
package: anyhow
version: 1.0.100
source: cargo registry, C:\Users\you\.cargo\registry\src\index.crates.io-1949cf8c6b5b557f\anyhow-1.0.100

declared: src/error.rs:81
signature:
pub fn msg<M>(message: M) -> Self
where
    M: Display + Debug + Send + Sync + 'static,
```

Without `name`, `api` gives the package's doc and lists its names:

```text
package: github.com/pelletier/go-toml/v2
version: v2.2.2
source: module cache, C:\Users\you\go\pkg\mod\github.com\pelletier\go-toml\v2@v2.2.2
doc:
Package toml is a library to read and write TOML documents.
names (12, the first 12 shown): Marshal, Unmarshal, DecodeError, Decoder, NewDecoder, Encoder, NewEncoder, Key, LocalDate, LocalDateTime, LocalTime, StrictMissingError
```

A package the project does not hold says so, with the command that adds it:

```text
api: github.com/google/uuid is not required by C:\work\shop\go\go.mod. install it with: go get github.com/google/uuid
```

## eval

`eval` answers what an expression returns in this project, without a file
or a `main` written by hand. It returns the value, its type, what it printed
and how long it took, and the working tree never changes. For Go it builds a
temporary `main` beside the package at `path` through `go`'s `-overlay`, so
the expression names the package; for TypeScript it imports the file at
`path` and awaits a promise; for Python it imports the file at `path` with
the project's Python; for Rust it builds a temporary binary in a copy of the
crate, and the value needs `Debug`.

```bash
tofu tool eval '{"lang":"go","path":"orders","expr":"orders.Order{Quantity: 3, Cents: 250}.Total()"}' --dir .
```

```text
value: 750
type: int
ms: 0.000
stdout: none
ran go run -overlay=...\overlay.json ./tofueval in C:\work\shop, 512 ms; the working tree was not changed
```

`setup` holds statements run before the expression, and a compile or syntax
error comes back as `expression:line:column` or `setup:line:column`.

## Parameters

The model calls these tools. `tofu tool` runs one by hand and prints what
the model would read. See [One tool call by hand](/docs/cli/tool).

| Tool | Parameter | What it does |
|---|---|---|
| `symbols` | `name` | the identifier, as spelled in the source; `Type.method` for a method; required |
| `symbols` | `path` | narrow the lookup to a file or directory |
| `symbols` | `mode` | `find` when left out, `impact` or `rename` |
| `symbols` | `to` | the new name, for `rename` |
| `symbols` | `apply` | write the rename rather than preview it |
| `symbols` | `include_ignored` | read ignored paths too |
| `map` | `task` | the work in plain words; its names seed the rank |
| `map` | `files` | paths that seed the rank |
| `map` | `budget` | tokens, 1,000 when left out |
| `map` | `path` | where to start |
| `api` | `package` | the module, package or crate; required |
| `api` | `name` | one name, or `Container.member`; left out lists the names |
| `api` | `lang` | `go`, `ts`, `rust` or `python`, when the directory holds more than one kind of project |
| `api` | `path` | the project directory |
| `eval` | `lang` | `go`, `ts`, `python` or `rust`; required |
| `eval` | `expr` | the expression whose value you want; required |
| `eval` | `path` | the Go package, TypeScript or Python file, or Rust crate it runs in |
| `eval` | `setup` | statements run before the expression |
| `eval` | `imports` | more Go imports; the standard packages it names are imported |
