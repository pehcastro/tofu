Build a notes API stored in SQLite with `bun:sqlite`.

- The database path comes from the env var `NOTES_DB`, defaulting to `notes.db`. Export `openDb(path: string)` from `src/db.ts` that opens the database and runs migrations, and make the app use the database opened at the path in `NOTES_DB` the first time a request arrives.
- Migrations: a `migrations` table records applied versions. Migration 1 creates `notes(id integer primary key, title text not null, body text not null, created_at text not null)`. Migration 2 adds a `tags text not null default ''` column. Running migrations twice changes nothing.
- `POST /notes` `{title, body, tags?: string[]}` returns 201 with the note including `id`, `tags` (an array) and `createdAt`.
- `GET /notes?q=word` searches title and body case-insensitively; `GET /notes?tag=t` filters by tag. `GET /notes/:id`, `PUT /notes/:id`, `DELETE /notes/:id` (204), with 404 for unknown ids.
- Write tests with `bun test` that use a temporary database file.
