---
id: rust-verify
domain: dev
document: Apollo's Rust handbook, chapter 2 on clippy and lint configuration; the cargo-workflows and rust-sanitizers-miri skills in low-level-dev-skills; the ECC rust-build-resolver agent; each command and flag checked against cargo, clippy and miri as they ship
found: local clones of the three collections, kept outside this repository
---

# The cargo gate and where it bends

Read the project's gate before using this one. An `xtask`, a Justfile, a Makefile or a CI workflow that names a lint command, a feature set or a test runner wins: it is what the project's own review runs.

## Each edit

```sh
cargo check --all-targets
cargo clippy --all-targets -- -D warnings
cargo test <filter>
cargo fmt --check
```

`--all-targets` takes in tests, examples and benches, which a bare `cargo check` skips, so a change that breaks a test only fails there. In a workspace, add `-p <crate>` to stay on the crate you changed, and `--workspace` once at the end. `-D warnings` makes every warning an error for this run only; it does not change the project's lint levels.

`cargo test <filter>` runs every test whose path contains the filter, across unit, integration and doc tests. `cargo test --test <name>` runs one integration test file and `cargo test --lib` only the unit tests. When the project uses nextest, `cargo nextest run <filter>` replaces it, and `cargo test --doc` runs the doctests nextest does not.

## Once at the end

- The crate's whole test run.
- `cargo check --no-default-features` and `cargo check --all-features` when the change touched a feature. Features add up: one enabled anywhere in the graph is enabled for every crate that sees it, so a change can pass with the defaults and fail without them. Some crates declare features that cannot be on together; `--all-features` then fails by design, and the project's CI shows which sets it builds instead.
- `cargo doc --no-deps` for a library, which catches a broken intra-doc link.
- `--locked` on the check after a dependency change, so cargo refuses to rewrite Cargo.lock and a lockfile out of step with the manifest fails here rather than in CI.

## Only when the diff warrants it

- **Unsafe code**: `cargo +nightly miri test <filter>`. Miri interprets the program and reports undefined behaviour the tests reach: a dangling pointer, a read of uninitialised memory, an invalid enum value, an aliasing violation. It runs in isolation, refusing the clock, the environment and the file system, so a test that needs them runs with `MIRIFLAGS=-Zmiri-disable-isolation`. It cannot call most foreign functions, so a test that crosses FFI is skipped with `#[cfg_attr(miri, ignore)]`. It is slow: always give a filter.
- **A dependency change**: `cargo deny check` or `cargo audit`, whichever the project has. Add, remove and upgrade with `cargo add`, `cargo remove` and `cargo update -p <crate>`, never by editing the version by hand.

Each of these needs a tool the machine may not have. When it is missing, say the step was not run; never report it as passed.

## Formatting

`cargo fmt --check` prints a diff and exits non-zero when a file is not formatted. Options in rustfmt.toml marked unstable apply only on a nightly rustfmt; a stable rustfmt warns and ignores them, so the check can disagree between toolchains. Run it on the toolchain `rust-toolchain.toml` names.

## Reading an error

Read the first error, with its code. `rustc --explain E0502` prints the full explanation with a worked example. `cargo clippy --explain <lint>` does the same for a lint and is the way to confirm a lint exists before naming it. Fix one error, check again, and stop after three attempts at the same one: report it with the output rather than reshaping the design to get past it.

## Lint levels

A crate's levels live in `[lints.rust]` and `[lints.clippy]` in Cargo.toml, or `[workspace.lints]` with `lints.workspace = true` in each member. `clippy.toml` sets lint options, such as `msrv` or `allow-unwrap-in-tests`, not levels. Many useful lints are in the `restriction` and `pedantic` groups and off by default. To check a change against one without editing the project, pass it for that run: `cargo clippy -- -W clippy::unwrap_used`, and read only the warnings in the files you changed.
