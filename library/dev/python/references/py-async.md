---
id: py-async
domain: dev
document: agents-knowledge async-python-patterns, its details reference, and python-anti-patterns; the ruff explanations of the ASYNC rules and RUF006 on ruff 0.16.10
found: local clones of the two collections, kept outside this repository
---

# asyncio

One thread runs every task, and a task gives the loop back only at an `await`. Anything between two awaits runs alone, and anything that blocks there stops every other task. Stay async or stay sync along a call path: an async function that calls blocking code, or a sync one that starts its own loop, hides the cost from both sides.

## Nothing blocks the loop

| Blocking | Async |
| --- | --- |
| `time.sleep(n)` | `await asyncio.sleep(n)` |
| `requests.get(url)` | `httpx.AsyncClient` or `aiohttp`, one client reused |
| `subprocess.run(args)` | `await asyncio.create_subprocess_exec(*args)` |
| a sync driver or SDK | its async API, or `await asyncio.to_thread(call, *args)` |
| CPU work over a few milliseconds | `await asyncio.to_thread(...)`, or a process pool when it holds the GIL |

Small file reads at start-up are fine; a large or slow one goes to `to_thread`. A coroutine called without `await` never runs and returns a coroutine object, and Python warns `coroutine ... was never awaited` only when it is collected.

## Every task has an owner

```python
async with asyncio.TaskGroup() as tg:
    for url in urls:
        tg.create_task(fetch(client, url))
```

`TaskGroup`, from 3.11, waits for every task, cancels the rest when one fails, and raises the failures as an `ExceptionGroup`. Below 3.11, `await asyncio.gather(*coros)` waits for all; with `return_exceptions=True` it returns failures in the result list instead of raising the first, so check each item.

A task started to outlive the current call is kept in a set and removed when done, `tasks.add(t)` then `t.add_done_callback(tasks.discard)`, and its owner awaits or cancels what is left at shutdown. A dropped `create_task` result can be collected mid-run; ruff reports it as `RUF006`.

Bound concurrency over many inputs with `asyncio.Semaphore(n)` held around the call, never one task per input with no limit.

## Cancellation and timeouts

Cancellation arrives as `asyncio.CancelledError` at the next `await`. It derives from `BaseException`, so `except Exception` lets it through. A handler that catches it for cleanup re-raises it; swallowing it leaves the canceller waiting forever. Put cleanup in `finally` or `async with`, which run on cancellation too.

`async with asyncio.timeout(5):`, from 3.11, cancels the block and raises `TimeoutError`; below 3.11, `await asyncio.wait_for(coro, 5)`. Every network await has one, directly or through the client's own timeout.

## Shared state

Between two awaits no other task runs, so a plain counter needs no lock there. A read, an `await`, then a write based on the read is a race: hold an `asyncio.Lock` across it. Never hold a `threading.Lock` across an `await`. Pass work between tasks through `asyncio.Queue`, whose `maxsize` gives back-pressure.

## Sync and async meet

`asyncio.run(main())` is called once, from sync code, at the top. It fails inside a running loop, so library code never calls it. From a thread outside the loop, `asyncio.run_coroutine_threadsafe(coro, loop)` hands work in.

## Testing it

A test that awaits one call at a time cannot see blocking. Start the call as a task, `await asyncio.sleep(0)` a few times, and assert another task made progress, or time a batch and compare it with one call.
