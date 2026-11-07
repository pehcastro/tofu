---
title: Changing code
description: codemod, scaffold, format and deps, the tools that rewrite one code shape everywhere, make a new file shaped like a sibling, run the project's own formatter on the files named, and add a dependency after reading its license, size and advisories.
order: 16
updated: 2026-11-07
---

Four tools change code the way a person means it rather than the way the
text happens to look. `codemod` rewrites one code shape everywhere on the
syntax tree. `scaffold` writes a new file shaped like a sibling. `format`
runs the project's own formatter on the files named and nothing else. `deps`
adds or upgrades a dependency after reading what it costs.

## Why they exist

A text replace cannot tell a name from the same letters inside a string, so
a rename by `edit` with `replace_all` changes message text too. A formatter
run over the whole directory through `bash` reformats files the person
wrote. `go get` or `npm install` through `bash` prints hundreds of lines and
never the license or the known advisories.

Renaming the local `got` to `actual` while keeping the message text, in Go,
TypeScript, Python and Rust, `codemod` changed 0 sites wrong and missed 0. `edit`
with `replace_all` changed 2 wrong in each language: the word `got` inside
the failure message.

## codemod

`codemod` takes a `pattern` and a `rewrite`, both code in `lang`, and
rewrites every match. It runs on cat-grep, which parses 47 languages inside
the tofu binary with nothing to install, the script code of Svelte, Vue and
PHP pages included. A string or a comment that looks like the pattern is
never touched.

In a pattern, `$NAME` matches one node, `$$$NAME` any run of nodes such as
call arguments or statements, and `$_` and `$$$` match without capturing. A
name used twice must match the same code, and every name in `rewrite` must
be captured in `pattern`.

### Preview, then apply

The first call previews every site with its line and a diff, and writes
nothing:

```bash
tofu tool codemod '{"lang":"go","pattern":"t.Fatalf($$$ARGS)","rewrite":"t.Errorf($$$ARGS)"}' --dir .
```

```text
codemod: 2 matches in 1 file. preview, nothing written
order_test.go:9 t.Fatalf("got %d, want %d", got, want) -> t.Errorf("got %d, want %d", got, want)
order_test.go:17 t.Fatalf("got %d, want %d", got, want) -> t.Errorf("got %d, want %d", got, want)
--- order_test.go
+++ order_test.go
@@ -6,7 +6,7 @@
 	got := Order{Quantity: 3, UnitCents: 250}.Total()
 	want := 750
 	if got != want {
-		t.Fatalf("got %d, want %d", got, want)
+		t.Errorf("got %d, want %d", got, want)
 	}
 }
 
@@ -14,7 +14,7 @@
 	got := Order{Quantity: 3, UnitCents: 250}.Discounted()
 	want := 675
 	if got != want {
-		t.Fatalf("got %d, want %d", got, want)
+		t.Errorf("got %d, want %d", got, want)
 	}
 }
 
to write these sites: the same call with "apply": true, "expect": 2
```

`apply` set to `true` writes the same sites and ends with the project's
checker. With `expect`, a count that differs from the preview is refused and
nothing is written:

```text
codemod: expected 3 matches and found 2, so nothing was written. preview again and apply with the new count
```

### A Rust rename reaches format strings

A Rust rename also renames the name inside a format string, such as
`{got}` in `assert_eq!`, and the `cargo check --all-targets` after it checks
the test code too:

```bash
tofu tool codemod '{"lang":"rust","pattern":"got","rewrite":"actual","paths":["src/lib.rs"],"apply":true,"expect":6}' --dir .
```

```text
codemod: 6 matches in 1 file. applied
src/lib.rs:26 got -> actual
src/lib.rs:28 got -> actual
src/lib.rs:28 got -> actual
src/lib.rs:33 got -> actual
src/lib.rs:35 got -> actual
src/lib.rs:35 got -> actual
--- src/lib.rs
+++ src/lib.rs
@@ -23,16 +23,16 @@
 
     #[test]
     fn total() {
-        let got = Order { quantity: 3, unit_cents: 250 }.total();
+        let actual = Order { quantity: 3, unit_cents: 250 }.total();
         let want = 750;
-        assert_eq!(got, want, "got {got}, want {want}");
+        assert_eq!(actual, want, "got {actual}, want {want}");
     }
 
     #[test]
     fn discounted() {
-        let got = Order { quantity: 3, unit_cents: 250 }.discounted();
+        let actual = Order { quantity: 3, unit_cents: 250 }.discounted();
         let want = 675;
-        assert_eq!(got, want, "got {got}, want {want}");
+        assert_eq!(actual, want, "got {actual}, want {want}");
     }
 
     #[test]
checker: typecheck: cargo check --all-targets in 278 ms: 0 errors
```

The words `got` and `want` in the message text are left alone.

### Match on more than one condition with rule

`rule` in place of `pattern` matches by `pattern`, `kind`, a node kind of
`lang`, and `regex` over the node text, combined with `all`, `any` and
`not`. `inside` and `has` take a rule plus `stopBy`, `neighbor` by default,
`end`, or a rule to stop at, and `field`, a field name of `lang`. The
matched node is replaced whole by `rewrite`. Here `got` is renamed only
inside the function that holds `test_discounted`:

```bash
tofu tool codemod '{"lang":"python","rule":{"pattern":"got","inside":{"kind":"function_definition","has":{"pattern":"test_discounted"},"stopBy":"end"}},"rewrite":"actual"}' --dir .
```

```text
codemod: 3 matches in 1 file. preview, nothing written
test_order.py:13 got -> actual
test_order.py:15 got -> actual
test_order.py:15 got -> actual
--- test_order.py
+++ test_order.py
@@ -10,9 +10,9 @@
         self.assertEqual(got, want, f"got {got}, want {want}")
 
     def test_discounted(self):
-        got = Order(quantity=3, unit_cents=250).discounted()
+        actual = Order(quantity=3, unit_cents=250).discounted()
         want = 675
-        self.assertEqual(got, want, f"got {got}, want {want}")
+        self.assertEqual(actual, want, f"got {actual}, want {want}")
 
     @unittest.skip("shipping rates are not modelled yet")
     def test_shipping(self):
to write these sites: the same call with "apply": true, "expect": 3
```

### Script code in Svelte, Vue and PHP pages

The script inside a Svelte, Vue, HTML or PHP page is matched with `lang`
set to its own language, `typescript`, `javascript` or `css`:

```bash
tofu tool codemod '{"lang":"typescript","pattern":"console.log($A)","rewrite":"console.info($A)","paths":["web"]}' --dir .
```

```text
codemod: 1 match in 1 file. preview, nothing written
web/Total.svelte:3 console.log(got) -> console.info(got)
--- web/Total.svelte
+++ web/Total.svelte
@@ -1,6 +1,6 @@
 <script lang="ts">
   let got = 1;
-  console.log(got);
+  console.info(got);
 </script>
 
 <p>got {got}</p>
to write these sites: the same call with "apply": true, "expect": 1
```

`lang` set to `svelte` matches the page markup itself, such as `<p>$$$A</p>`.

A language cat-grep does not parse is refused with the list it does, each
with its short names, such as `go (golang)`, `typescript (ts)` and
`rust (rs)`.

## scaffold

`scaffold` copies the file named in `like`, beside it, with the sibling's
name replaced by `name` in every case form: PascalCase, camelCase,
snake_case, kebab-case and SCREAMING_SNAKE, in the contents and the file
name. The sibling's name is its file name before the first dot. A name
inside a longer word, as `order` in `reorder` or `orders`, is left alone.

```bash
tofu tool scaffold '{"kind":"type","name":"Invoice","like":"order.go"}' --dir .
```

```text
scaffold: type Invoice like order.go, wrote invoice.go
replaced Order -> Invoice (PascalCase): 3 in the contents
--- invoice.go
+++ invoice.go
@@ -1,0 +1,16 @@
+package orders
+
+import pr "fixture.test/orders/pricing"
+
+type Invoice struct {
+	Quantity  int
+	UnitCents int
+}
+
+func (o Invoice) Total() int {
+	return pr.Total(o.Quantity, o.UnitCents)
+}
+
+func (o Invoice) Discounted() int {
+	return o.Total() - 100
+}
```

The package name `orders` is left alone. A target that already exists is
refused:

```text
scaffold: invoice.go already exists, so nothing was written. pick another name, or edit that file
```

## format

`format` runs the project's own formatter over the files in `paths` and
nothing else: gofmt for Go, rustfmt for Rust, prettier for TypeScript,
JavaScript, JSON, CSS and Markdown, and ruff for Python. Without `paths` it
formats the files this turn changed. Each file is reported formatted,
unchanged, skipped or not formatted, with the reason.

```bash
tofu tool format '{"paths":["label.go","order.go","README.md"]}' --dir .
```

```text
format: formatted 1 of 3 files from the paths given
formatted by gofmt: label.go
unchanged by gofmt: order.go
skipped, it does not exist: README.md
```

gofmt is inside tofu. The others are the project's own: prettier from
`node_modules` or `PATH`, ruff from the project's `.venv` or `PATH`, rustfmt
from `PATH`, with the edition `Cargo.toml` names. A missing one is named with
how to install it:

```text
format: formatted 0 of 1 files from the paths given
not formatted, prettier is not installed: it is not under node_modules in this directory or above it, nor on PATH. install it with npm install --save-dev prettier, then call format again: src/order.ts
```

## deps

`deps` adds, upgrades, explains or lists the project's dependencies for Go,
npm, cargo and Python through uv. `add` and `upgrade` read the registry,
the license from deps.dev and the known advisories from osv.dev first, then
run the package manager and report what changed.

```bash
tofu tool deps '{"mode":"add","package":"github.com/google/uuid"}' --dir .
```

```text
deps add github.com/google/uuid
ecosystem: go
from: none
to: v1.6.0
license: BSD-3-Clause (deps.dev)
size: 31981 bytes (proxy.golang.org)
advisories: 0 on v1.6.0 (osv.dev)
ran: go get github.com/google/uuid@v1.6.0
lockfile_changed: go.mod changed, go.sum created
```

A version with a known advisory is refused with the versions that fix it,
and nothing is written:

```text
deps add golang.org/x/text
ecosystem: go
from: none
to: v0.3.7
license: BSD-3-Clause (deps.dev)
size: 8610883 bytes (proxy.golang.org)
advisories: 3 on v0.3.7 (osv.dev)
  GHSA-69ch-w2m2-3vjp CVE-2022-32149 GO-2022-1059 HIGH: golang.org/x/text/language Denial of service via crafted Accept-Language header, fixed in 0.3.8
  GO-2026-5970 CVE-2026-56852: Infinite loop on invalid input in golang.org/x/text, fixed in 0.39.0
  GO-2026-6629 CVE-2026-56851: Panic parsing crafted input in x/text/secure/precis in golang.org/x/text, fixed in 0.41.0
wrote nothing: v0.3.7 has known advisories. name a fixed version, or pass accept_advisories true to take it anyway
```

`outdated` lists direct dependencies with a newer release, and `upgrade`
says what moves:

```text
deps outdated: 1 of 1 direct dependencies have a newer release (proxy.golang.org, osv.dev)
github.com/google/uuid v1.3.0: v1.6.0 is out
```

```text
deps upgrade github.com/google/uuid
ecosystem: go
from: v1.3.0
to: v1.6.0
license: BSD-3-Clause (deps.dev)
size: 31981 bytes (proxy.golang.org)
size_delta: +6954 bytes
license_from: BSD-3-Clause (deps.dev)
advisories: 0 on v1.6.0 (osv.dev)
advisories_from: 0 on v1.3.0
ran: go get github.com/google/uuid@v1.6.0
lockfile_changed: go.mod changed, go.sum changed
```

`why` names what pulls a package in. A registry that does not answer is
named and nothing is written.

## Parameters

The model calls these tools. `tofu tool` runs one by hand and prints what
the model would read. See [One tool call by hand](/docs/cli/tool).

| Tool | Parameter | What it does |
|---|---|---|
| `codemod` | `lang` | the language of the pattern; required |
| `codemod` | `pattern` | the code shape to match, with meta variables |
| `codemod` | `rule` | in place of `pattern`: `pattern`, `kind`, `regex`, `all`, `any`, `not`, `inside`, `has` |
| `codemod` | `rewrite` | the code that replaces each match; required |
| `codemod` | `paths` | files or directories, the whole working directory when left out |
| `codemod` | `apply` | write the sites rather than preview them |
| `codemod` | `expect` | refuse the write when the match count differs |
| `scaffold` | `like` | the sibling file to copy; required |
| `scaffold` | `name` | the new name; required |
| `scaffold` | `kind` | what is made, such as component, handler or test; required |
| `format` | `paths` | the files to format, the files this turn changed when left out |
| `deps` | `mode` | `add`, `upgrade`, `why` or `outdated`; required |
| `deps` | `package` | module path, npm name, crate or PyPI name; `name@version` is accepted |
| `deps` | `version` | the newest stable release when left out |
| `deps` | `ecosystem` | `go`, `npm`, `cargo` or `python`, when the directory holds more than one kind of project |
| `deps` | `accept_advisories` | take a version with a known advisory anyway |
