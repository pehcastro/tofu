# notes

A small notes service. It has no runtime dependency: the router is forty lines in `src/router.ts`.

## Running

`src/index.ts` exports the app. Nothing binds a port; the tests drive it with `app.request()`.

- `bun test` runs the suite.
- `bun run check` type-checks without emitting.

## Routes

- `GET /` returns the plain text `notes`.
- `POST /notes` creates a note from `{ "title": string, "body": string }`.
- `GET /notes` lists notes, newest created first.
- `GET /notes/:id` fetches one note.
- `PATCH /notes/:id` updates `title` and/or `body`.
- `DELETE /notes/:id` removes a note.

Invalid input is HTTP 400 with `{ "error": string }`. An unknown id is HTTP 404 with the same shape.

## Decisions

The standing decisions about this service are in `docs/decisions.md`. They are not suggestions and they are the reason several obvious designs are not used here.
