---
id: py-errors-and-resources
domain: dev
document: agents-knowledge python-error-handling, python-resource-management, python-resilience and python-anti-patterns; trail of bits sharp-edges lang-python; each ruff code checked against ruff rule on ruff 0.16.10
found: local clones of the two collections, kept outside this repository
---

# Exceptions and resources

## Raise what the caller can tell apart

Raise a built-in type when it says what failed: `ValueError` for a bad value, `TypeError` for a wrong type, `KeyError` or `LookupError` for a missing entry, `TimeoutError`, `FileNotFoundError`. When callers must tell your failures from everyone else's, add one base class for the package and a subclass per failure they handle differently, and nothing deeper. Follow what the package already has before adding either.

The message says what failed and the value that made it fail: `ValueError(f"page_size must be 1 to 100, got {size}")`. Never `raise Exception(...)`: no caller can catch it without catching everything.

## Catch narrowly, once

Put the `try` around the line that can fail, not the whole function, and catch only the types you handle there. `try` / `except` / `else` keeps the code that runs on success out of the guarded block, so its failures are not caught by mistake.

| You want | Write |
| --- | --- |
| the same exception to continue | `raise` alone, which keeps the traceback |
| your own type with the cause attached | `raise StoreError(key) from err` |
| your own type, the cause hidden | `raise StoreError(key) from None` |

Handle an exception once: log it or raise it, never both, or every frame prints the same failure. At a boundary that must keep going, `logger.exception("...")` logs the message with the traceback. `assert` is removed under `python -O`, so it never checks input; raise instead.

In a batch where one item's failure must not stop the rest, collect the failures beside the results and return both, so the caller sees what failed rather than a shorter list.

## Exception groups

`asyncio.TaskGroup` and `ExceptionGroup` raise several failures at once, from 3.11. `except* ConnectionError:` handles the matching members and lets the rest continue up. A plain `except ConnectionError:` does not match a group holding one.

## Release with `with`

Anything that holds an operating system resource or a lock is used through `with`: files, sockets, `threading.Lock`, database connections and cursors, `tempfile.TemporaryDirectory`, an HTTP client session. `__exit__` runs when the block ends by any path, so the release cannot be skipped by an early `return` or an exception.

```python
@contextlib.contextmanager
def connection(dsn: str) -> Iterator[Connection]:
    conn = connect(dsn)
    try:
        yield conn
    finally:
        conn.close()
```

Without the `try` / `finally`, an exception raised in the caller's block comes back out of the `yield` and skips the close. An `__exit__` that returns `True` swallows the exception; return `None` unless suppressing is the point, and then say so in the name, as `contextlib.suppress` does.

`contextlib.ExitStack` holds a number of resources known only at run time and releases them in reverse order. `contextlib.closing` adapts an object that has `close` but no `__exit__`.

## The traps

- `finally` with `return` discards the exception being raised. Never return from `finally`.
- A generator holding a resource releases it only when exhausted or closed. Wrap its use in `contextlib.closing`, or read it fully inside the `with`.
- `os.environ["X"]` raises `KeyError` far from where the setting was meant to be read. Read configuration once at start-up into a typed object and fail there with the variable's name.
- `except Exception` does not catch `KeyboardInterrupt`, `SystemExit` or `asyncio.CancelledError`, which derive from `BaseException`. Catching `BaseException` catches all three, so re-raise it.
