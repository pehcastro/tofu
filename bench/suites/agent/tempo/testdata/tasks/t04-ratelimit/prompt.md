Add a fixed-window rate limiter middleware for every route.

- The limit is 5 requests per 60 seconds per client. The client key is the `x-forwarded-for` header, or `"local"` when it is absent.
- Every response carries `X-RateLimit-Limit`, `X-RateLimit-Remaining` headers.
- The sixth request in a window returns 429 with `{"error":"rate limited"}` and a `Retry-After` header in whole seconds.
- Export a function `resetRateLimits()` from `src/ratelimit.ts` that clears all counters, for tests.
- The clock must be injectable for tests: export `setNow(fn: () => number)` from `src/ratelimit.ts`.
- Write tests with `bun test`, including the window reset.
