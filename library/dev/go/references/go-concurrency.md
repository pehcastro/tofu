---
id: go-concurrency
domain: dev
document: goroutine lifetimes, channels, locks, atomics and errgroup, with the vet analyzers that check them
---

# Goroutines, channels and locks

Add a goroutine only when the work is measurably slow done in sequence or must run beside something else. Every one has an owner, a way to stop and someone who waits for it; a goroutine without all three is a leak or a race waiting for load.

## Before `go`

- **How does it stop?** `<-ctx.Done()`, a closed input channel or a done channel. Each blocking `select` in it has one of those as a case.
- **Who waits for it?** The function that starts it, or a type whose `Close` or `Shutdown` waits, never nobody.
- **Where do its errors go?** Returned through `errgroup`, sent on a channel the owner reads, or handled inside. A panic in a goroutine ends the process, and only a `recover` deferred in that same goroutine catches it.

## Waiting

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(8)
for _, item := range items {
	g.Go(func() error { return process(ctx, item) })
}
return g.Wait()
```

`errgroup` from `golang.org/x/sync/errgroup` returns the first error and, with `WithContext`, cancels the rest. `SetLimit` bounds how many run, which replaces a hand-written worker pool. Use `sync.WaitGroup` when there are no errors to collect: `wg.Add(1)` before the `go` statement, `defer wg.Done()` first in the goroutine, or `wg.Go(f)` from Go 1.25. `Add` inside the goroutine races with `Wait`, which can return before it runs; vet's `waitgroup` reports it.

From the go.mod `go` line 1.22 on, each loop iteration has its own variable, so the closure above captures `item` safely. Below 1.22, pass it as an argument or copy it first.

## Channels

- The sender closes, and only once. A send on a closed channel panics, so a receiver never closes.
- A receive from a closed channel returns the zero value at once. In a `select` loop that turns into a busy loop: read with `v, ok := <-ch` and set `ch = nil` when `!ok`, which disables that case.
- A `select` with a `default` inside a `for` never blocks and spins a core. Drop the `default` and wait on the channel and `ctx.Done()` instead.
- `break` inside a `select` or `switch` leaves only the `select`. To leave the loop, label it and `break loop`, or `return`.
- A send nobody receives blocks its goroutine forever. Size a buffer to what the producer may get ahead by, or select on `ctx.Done()` beside the send.
- Use the direction types, `chan<- T` and `<-chan T`, in parameters, so the compiler refuses a send where only receives belong.
- `time.After` in a loop creates a timer per turn. Hold one `time.NewTimer` and `Reset` it, or use a `time.Ticker`.

## Locks

A `sync.Mutex` lives beside the fields it guards, and every read and write of those fields holds it, reads included: a concurrent map read and write is a fatal error `recover` cannot stop. Hold it for the shortest span, never across I/O, a channel operation or a call back into code that may take the same lock. Two locks are always taken in the same order.

`sync.RWMutex` helps only when reads far outnumber writes and are slow; it cannot upgrade a read lock to a write lock, and trying deadlocks. `atomic.Int64`, `atomic.Bool` and `atomic.Pointer[T]` suit one value changed alone. `sync.Once` and `sync.OnceValue` run a lazy initialiser exactly once.

A value holding a lock, a `WaitGroup` or a `Once` is never copied: pointer receivers, passed by pointer, never ranged over by value. vet's `copylocks` reports a copy.

## Proving it

`go test -race` reports a race only when the test drives both sides at once, so a fix for a race comes with a test that starts the goroutines together, often under `t.Parallel()` or with a `sync.WaitGroup` release. When the project uses `go.uber.org/goleak`, `defer goleak.VerifyNone(t)` fails a test that leaves a goroutine running. `t.Fatal` from a goroutine the test started stops only that goroutine; vet's `testinggoroutine` reports it, and the fix is to send the failure back to the test's goroutine.
