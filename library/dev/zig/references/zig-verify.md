---
id: zig-verify
domain: dev
document: checking Zig code: zig fmt, ast-check, build check, build test, filters, release-safe tests
---

# The zig gate and where it bends

Read the project's gate first. A `check` or `fmt` step in build.zig, a test-filter option, a linter config or a CI workflow wins over this page. `zig build -l` lists the steps.

## What each command catches

Costs are wall time on a `zig init` project on 0.16-dev, warm cache unless marked cold.

| Command | Catches | Never catches | ms |
|---|---|---|---|
| `zig fmt --check --ast-check <files>` | layout (prints file names, no diff), parse errors, unused local or parameter, `var` never mutated | any type error | 58 |
| `zig ast-check file.zig` | the same, one file | any type error | 55 |
| `zig build check` | every semantic error in code reachable from the checked root, no codegen; only where the project defines the step | code nothing reaches | 163 cached, 1033 after an edit |
| `zig build` | the same plus codegen and link, installs to `zig-out/` | code nothing reaches | 168 cached, 2710 after an edit |
| `zig build test --summary all` | failing tests, leaks under `std.testing.allocator`, safety panics | tests in files the root does not reach | 271 cached, 3157 cold |
| `zig test file.zig` | the tests of one file | a file that needs build-system imports such as `build_options`: use `zig build test` | 107 cached, about 3000 to compile |
| `zig build test -Doptimize=ReleaseSafe` | overflow and cast checks with optimisation | | about 10000 |

A missing `try` and a discarded error union are type errors, so ast-check passes them and the build refuses them. A swallowed error (`catch {}`, `catch 0`), an unflushed writer, a `{}` on a type with a `format` method and a type error in a function nothing calls all compile and pass.

## Tests

- `zig build test` with the default summary prints nothing when it passes. Always pass `--summary all` and read the count.
- `zig build test` has no filter of its own. `--test-filter` is refused as an unrecognized argument, and `-- --test-filter x` is silently ignored and runs every test. `-Dtest-filter=<name>` works only where build.zig declares `b.option(..., "test-filter", ...)` and passes it to `addTest(.filters)`; `zig init` does not. An unnamed `test {}` block runs under any filter.
- A leak prints the test as OK and `All 1 tests passed.`, then `1 tests leaked memory.` and exit 1. Read the last lines and the exit code, not the first.
- ReleaseFast drops the safety checks: an overflow that panics in Debug and ReleaseSafe wraps to 0 and exits 0. Test a change to arithmetic or casts in ReleaseSafe (`-Doptimize=safe` on 0.17 and later).
- A missing `errdefer` shows only under `std.testing.checkAllAllocationFailures`, which fails each allocation in turn.

## Reading an error

Read the first error. A stale API is always a compile error of the shape `has no member named`, `no field named` or `invalid builtin function`: look the name up in the churn reference and in the std source at `.std_dir` from `zig env`. An error whose location is inside std, such as `invalid format string 's' for type`, was caused by your call: rerun with `-freference-trace` to see it.

## The cache

Every `zig build`, and `zig test` below a build.zig, writes `.zig-cache/` in the project, and it grows into hundreds of megabytes across optimize modes. `ZIG_LOCAL_CACHE_DIR` and `ZIG_GLOBAL_CACHE_DIR` move it; `--cache-dir` and `--global-cache-dir` are refused by tofu's read list, so the environment is the lever. `zig-cache/` is the name before 0.13 and `zig-pkg/` holds fetched dependencies from 0.16.

## Not on every machine

`--fuzz` is not implemented on Windows and `-fsanitize-thread` does not build there. When a step needs a tool the machine lacks, say it was not run; never report it as passed. Linters exist only where the project carries `zlint.json`, `.ziglint.zon` or a zlinter step: run the one it has.
