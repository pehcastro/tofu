`src/index.ts` is one large file. Refactor it without changing behaviour:

- Move each resource into its own module under `src/routes/` (one Hono sub-app per resource, mounted with `app.route`).
- Move shared validation helpers into `src/lib/validate.ts` and the in-memory stores into `src/lib/store.ts`.
- Remove the duplicated id parsing and not-found handling by writing one helper used by every route.
- Every existing response (status codes, JSON shapes, error messages) must stay exactly the same. Write tests with `bun test` that pin the current behaviour BEFORE you refactor, then keep them passing.
