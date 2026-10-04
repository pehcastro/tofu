---
id: rust-async
domain: dev
document: async Rust: blocking, cancel safety, select! and locks across an await
---

# Async Rust: cancellation, ordering and backpressure

The examples use tokio, the runtime most projects have. Read Cargo.toml for the one this project uses; async-std and smol have the same hazards under other names.

## Nothing blocks the executor

A runtime runs many tasks on a few threads. A blocking call in one task stops every task on that thread. Inside async code use `tokio::fs`, `tokio::time::sleep` and tokio's sockets, and move blocking or CPU-heavy work to `tokio::task::spawn_blocking`. `block_in_place` also works, but only on the multi-thread runtime: it panics on a current-thread one.

A `std::sync::Mutex` is fine in async code when its guard is dropped before the next `.await`. Hold it across one and the task can be suspended holding the lock, and the future is no longer `Send`. Use `tokio::sync::Mutex` only when the lock must span an await; it is slower for everything else.

## Every await is a place to stop

A future can be dropped at any `.await`: by the losing branch of `select!`, by `tokio::time::timeout`, or by `JoinHandle::abort`. Code after that await never runs. So this leaves state half written when it is cancelled:

```rust
self.in_flight += 1;
self.sink.send(item).await?;
self.in_flight -= 1;
```

Make the change whole on one side of the await, or restore it in a `Drop` guard. tokio documents for each method whether it is cancel safe: `mpsc::Receiver::recv` is, so it can sit in a `select!` loop; `read_exact` and `read_line` are not, and lose what they had read when dropped. In a loop, create a long-running future once, `tokio::pin!(fut)`, and poll `&mut fut` in the `select!`, so each turn of the loop does not start it again.

## select! picks at random

When several branches are ready, `tokio::select!` picks one at random. When shutdown must win, add `biased;` as the first line and put the shutdown branch first: the branches are then polled from the top.

```rust
tokio::select! {
    biased;
    _ = shutdown.changed() => break,
    Some(job) = jobs.recv() => run(job).await?,
}
```

`futures::select!` has no `biased;`; its ordered form is the separate `futures::select_biased!` macro.

## Bounded channels

`mpsc::channel(n)` holds at most `n` messages, and `send().await` waits when it is full, which slows the producer to the consumer's pace. `unbounded_channel` never waits, so a slow consumer turns into memory that grows until the process dies. Use a bound unless the producer is bounded some other way, and choose what a full channel means: wait with `send().await`, or refuse with `try_send` and report it.

## Locks and tasks

- Take two locks in the same order everywhere. Code that takes `a` then `b` beside code that takes `b` then `a` deadlocks under load and passes every test.
- Call nothing that may take the same lock while holding it: a callback, a trait object, an `.await` on work that needs it.
- A dropped `JoinHandle` does not stop its task, and the task's error or panic is lost unless someone awaits the handle. Keep the handles, or use a `JoinSet`, and await them before returning.
