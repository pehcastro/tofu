---
id: rust-errors-and-ownership
domain: dev
document: rust-skills' m01-ownership skill and its table of borrow errors; Apollo's Rust handbook, chapters 1 and 4 on borrowing and error handling; the ECC rust-build-resolver agent; each error code checked with rustc --explain
found: local clones of the three collections, kept outside this repository
---

# Borrow errors and error types

## A borrow error is a question about ownership

Run `rustc --explain <code>` first. Then answer the question the error is asking before changing anything. The quick fix in the right column hides the problem when the question has a different answer.

| Error | What happened | Ask first | The quick fix to distrust |
|---|---|---|---|
| E0382 | a value was used after it moved | who should own it after this line | `.clone()` before the move |
| E0499 | two `&mut` to one value at once | can the two uses happen one after the other | `RefCell` |
| E0502 | a `&mut` while a `&` is still live | can the shared borrow end first | cloning what was read |
| E0505 | a value moved while borrowed | does the borrow need to outlive the move | cloning the borrowed value |
| E0506 | an assignment while borrowed | should the mutation happen elsewhere | ending the borrow by copying |
| E0507 | a move out of a reference | why is a borrowed value being taken | `.clone()`, where `std::mem::take` or `Option::take` may say it |
| E0515 | a reference to a local is returned | should the caller own the result | `'static` or `Box::leak` |
| E0597 | a value is dropped while still borrowed | does the value live in the right scope | widening the scope by hand |
| E0716 | a temporary is dropped while borrowed | why is this a temporary | binding it without asking |
| E0106 | a lifetime is missing | which input does the output borrow from | `'static` |

Shapes that usually settle it: end a shared borrow before taking a mutable one; borrow instead of moving; pass the value to the owner that outlives every user; split a struct so its fields are borrowed apart; collect the keys first, then mutate. When three attempts at one error fail, report it with the output; the design needs a decision, not a fourth attempt.

## Picking the ownership

| Need | Use |
|---|---|
| read only | `&T` |
| change in place | `&mut T` |
| the caller gives it away | `T` |
| read, and sometimes change, a borrowed value | `Cow<'_, T>` |
| several owners on one thread | `Rc<T>` |
| several threads | `Arc<T>`, with a `Mutex` or an atomic for change |

`.clone()` is right when two parts of the program really need their own copy. It is wrong when it exists to end a borrow error.

## Error types

A library returns its own error, an enum with one variant per failure a caller can act on:

```rust
#[derive(Debug, thiserror::Error)]
pub enum LoadError {
    #[error("config file is missing at {0}")]
    Missing(PathBuf),
    #[error("config is not valid TOML")]
    Parse(#[from] toml::de::Error),
}
```

`#[from]` lets `?` convert the lower error. A binary that only reports errors uses `anyhow::Result` and adds what it was doing with `.context("reading the config")?`. Keep both out of a crate that already does without them.

Without `unwrap`, a value that may be absent becomes an error at the point it is read:

```rust
let Some(port) = args.port else {
    return Err(LoadError::MissingPort);
};
let width = u16::try_from(raw).map_err(|_| LoadError::TooWide(raw))?;
```

## Testing an error

Every error path a change adds gets a test that reaches it. `unwrap_err()` is fine in a test. Assert on the variant when the type has no `PartialEq`, and on the message only when the message is the contract:

```rust
let err = load(Path::new("missing.toml")).unwrap_err();
assert!(matches!(err, LoadError::Missing(_)));
```

## Errors across tasks and threads

An error returned from a spawned task or sent to another thread must be `Send + Sync + 'static`. `Box<dyn Error>` is neither `Send` nor `Sync`; write `Box<dyn Error + Send + Sync>`. `anyhow::Error` already is. A task that panics hands back a `JoinError` from its `JoinHandle`, so a handle that is never awaited loses both its result and its panic.
