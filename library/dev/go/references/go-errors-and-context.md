---
id: go-errors-and-context
domain: dev
document: wrapping, comparing and handling errors, and passing a context through a call chain
---

# Errors and context

## An error is returned once, with what the caller lacks

Each function that cannot finish does one of three things with an error: handles it and carries on, returns it, or, at the top of the program, reports it and exits. Logging it and returning it as well prints the same failure once per frame.

When returning, add what the caller does not already know, usually the operation and its input, and wrap with `%w` at the end: `fmt.Errorf("open config %s: %w", path, err)`. When the wrap would only repeat the function name the caller already sees, return `err` unchanged. Error strings are lower case with no closing punctuation, because they are joined into longer messages.

| The caller needs | Return |
| --- | --- |
| only a message | `errors.New("...")` or `fmt.Errorf` |
| to recognise one fixed failure | a package-level `var ErrNotFound = errors.New("not found")` |
| to read fields of the failure | a type with an `Error() string` method |

Follow what the package already has before adding either kind. A caller tests with `errors.Is(err, ErrNotFound)` and `errors.As(err, &target)` with `target` declared as the pointer type; `errors.Join` keeps several causes and both functions see all of them.

## The nil that is not nil

An interface value is nil only when both its type and its value are nil. `var p *MyErr` returned as `error` carries the type, so `err != nil` is true and the caller treats success as failure. Return the literal `nil` on success, keep local error variables typed `error`, and return `error` rather than a concrete error type from exported functions.

## Values beside an error

When a function returns an error, its other results are unspecified unless its documentation says otherwise, so callers do not read them. Use `(T, bool)` or `(T, error)` rather than a magic value such as `-1` or `""` that a caller can forget to check.

## Context is the first parameter and is passed down

`ctx context.Context` comes first in every function that does I/O, waits or starts work, and every call below it receives the same `ctx`. A `context.Background()` deep in a call chain cuts the request's deadline and cancellation, so the work keeps running after the caller has gone. `Background` belongs in `main`, in tests and at the top of a goroutine that owns its own lifetime. A `nil` context panics on first use; pass `context.TODO()` where none exists yet.

A context is never a struct field: a struct outlives the request that built it, and its methods would run under a context already cancelled. The exception is a type whose method set an outside interface fixes.

## Every derived context is cancelled

```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
```

An uncalled `cancel` holds the child and its timer until the parent ends; vet's `lostcancel` finds the path that skips it. Return early on `ctx.Err()` in a long loop, and put `case <-ctx.Done(): return ctx.Err()` in every blocking `select`. To tell a timeout from a cancel, compare `errors.Is(err, context.DeadlineExceeded)`.

Work that must finish after the request ends, such as an audit write started by a handler, takes `context.WithoutCancel(ctx)`, which keeps the values and drops the cancellation. It has no deadline of its own, so give it one with `WithTimeout`.

## Values in a context

A context value is for data that belongs to the request and crosses an API that cannot carry it: a request id, a trace span, the authenticated caller. Anything else is a parameter. Keys are an unexported type, `type ctxKey struct{}`, so two packages cannot collide on a string; staticcheck's `SA1029` reports a built-in type used as a key.

## HTTP

A handler takes its context from `r.Context()`. After `http.Error(w, ...)` the handler keeps running: `return` on the next line. Outgoing calls use `http.NewRequestWithContext`, and a response body is closed after it is read, even when the status is an error; golangci-lint's `bodyclose` checks that.
