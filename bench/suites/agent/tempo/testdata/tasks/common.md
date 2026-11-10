

Rules for this task:
- This directory is a Hono app on Bun. Keep `export default app` in `src/index.ts`, where `app` is the Hono instance serving every route.
- Do not start a long-running server and leave it running. Use `app.request()` in tests, or start and stop a server inside one command.
- You may add dependencies with `bun add`.
- Work until the task is complete and you have checked it yourself, then stop.
