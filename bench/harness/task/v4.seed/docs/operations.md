# Operations

## Configuration

Everything tunable is in `src/config/defaults.ts` and may be overridden by an environment variable read in `src/config/env.ts`. A variable that is set to something unreadable is a startup failure, not a fallback.

## Deployment

One process, no state on disk, restarted on every release. The store is `src/db/memory.ts` and it is empty after a restart, which is fine while this is a staging build.

## Rate limits

The limiter is `src/http/middleware/rateLimit.ts`. It counts requests per account per minute against the ceiling in the defaults. A request over the ceiling is rejected with `usage.rate.exceeded`.

## Exports

An export runs in the request that created it. There is no queue and no worker. A large CSV blocks the process, which is the main reason the row ceilings exist.

## What to check when a customer says a request was refused

Read the code in the error body first. The status alone does not say which of several checks refused the request, and two of them share a status.
