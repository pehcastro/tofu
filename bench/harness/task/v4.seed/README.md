# meterage

Usage metering and reporting for accounts on a plan. It exposes a small HTTP API under `/v1`, keeps everything in memory, and has no runtime dependency.

## Layout

- `src/http` is the server: the router, the request context, the middleware chain and the route handlers under `src/http/routes/v1`.
- `src/plan` is what an account on a plan is allowed to do.
- `src/accounts` is the account directory.
- `src/usage` counts rows and windows them.
- `src/export` turns a report into a file in one of the export formats.
- `src/errors` holds the error codes the API returns and the error type that carries them.
- `src/config` holds the defaults and the environment overrides.
- `src/legacy` is the previous release's tables, kept for a migration that has not been written.
- `src/db` is the in-memory store.

## Running

- `bun test` runs the suite.
- `bun run check` type-checks without emitting.

## Documentation

`docs/api.md` describes the routes. `docs/plans.md` describes the plans. `docs/operations.md` describes deployment.

Documentation in this project is written by hand and is not checked against the code by anything.
