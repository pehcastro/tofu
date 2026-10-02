---
id: rust-unsafe
domain: dev
document: the before-unsafe, review-unsafe and common-pitfalls checklists in rust-skills' unsafe-checker; Trail of Bits rust-review, its unsafe-boundary cluster and its cstring-dangling and vec-set-len-uninit finders; the edition 2024 forms checked against the Rust edition guide
found: local clones of both collections, kept outside this repository
---

# Unsafe code, before and after

`unsafe` does not switch checks off. It lets a few more operations through, chiefly dereferencing a raw pointer, calling an `unsafe fn` or a foreign function, reading or writing a `static mut`, implementing an `unsafe trait` such as `Send`, and reading a `union` field. Each one is a promise the compiler cannot check, so the code beside it carries the proof.

## Before writing it

- Look for the safe form first: a restructure the borrow checker accepts, `Cell`, `RefCell` or a `Mutex`, a slice method, or a crate that already wraps the operation.
- Name the one operation that needs `unsafe` and keep the block to that operation.
- Write the precondition before the code: for a pointer, it is non-null, aligned for its type, inside one live allocation, initialised for its type, and not aliased by a live `&mut`.
- Ask what happens if any line panics: a structure half updated, a length counting slots never written, a `Drop` that sees invalid state.

## Reviewing it

- A `// SAFETY:` line on every block that names the precondition and where it was established. One that only says the code is safe is a missing comment.
- A `# Safety` section on every `pub unsafe fn`, listing what the caller must guarantee.
- No soundness condition guarded only by `debug_assert!`, which a release build removes.
- Every `unsafe impl Send` or `Sync` argued like a block: which state is shared, and what synchronises it.
- `cargo +nightly miri test <filter>` over the tests that reach the block, with the result in the report.

## Pitfalls

**A pointer into a dropped `CString`.** `let p = CString::new(s)?.as_ptr();` drops the `CString` at the end of the statement and leaves `p` dangling. Bind the owner, `let owned = CString::new(s)?;`, and pass `owned.as_ptr()` while it lives. A function cannot return `as_ptr()` of a local; hand ownership out with `into_raw()` and take it back with `CString::from_raw` to free it.

**`set_len` over memory never written.** `Vec::with_capacity(n)` followed by `set_len(n)` claims `n` initialised values that do not exist, and the next read, iteration or drop is undefined behaviour, for `u8` too. Write each slot through `spare_capacity_mut()` first, or use `resize` or `vec![value; n]` and skip `unsafe` entirely.

**Transmuting an integer into an enum.** Any value with no matching variant is undefined behaviour the moment it exists. Convert with a `match` or a `TryFrom` impl that returns an error for the rest.

**`Clone` on a type whose `Drop` frees a handle.** Two copies of the same raw pointer each free it, which is a double free. Do not derive or write `Clone` for such a type; share it with `Arc`, or count references explicitly.

**A reference to a packed field.** The compiler refuses `&p.field` on a `#[repr(packed)]` struct, since the field may be misaligned. Copy it out by value, `let value = { p.field };`, or read it with `unsafe { (&raw const p.field).read_unaligned() }`.

**A panic leaving an `extern "C"` function.** Since Rust 1.81 it aborts the process. A callback that can panic wraps its body in `std::panic::catch_unwind` and turns the panic into an error code the caller understands.

## Edition 2024

- Attributes that can break linking are written as unsafe: `#[unsafe(no_mangle)]`, `#[unsafe(export_name = "...")]`, `#[unsafe(link_section = "...")]`.
- An `extern` block is `unsafe extern "C" { ... }`, and an item inside it that is sound to call from any safe code can be declared `safe fn`.
- Inside an `unsafe fn`, each unsafe operation still needs its own `unsafe { }` block; `unsafe_op_in_unsafe_fn` warns without it.
- A reference to a `static mut`, `&STATIC` or `&mut STATIC`, is refused by the `static_mut_refs` lint. Prefer an atomic, a `Mutex` or a `OnceLock`; where a raw address is needed, take `&raw const STATIC` or `&raw mut STATIC`.
- `std::env::set_var` and `remove_var` are `unsafe`, since another thread may be reading the environment.

A project still on edition 2021 keeps the older forms. Read `edition` in Cargo.toml before writing either.
