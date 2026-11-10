Build an in-memory todo API under `/todos`.

- `POST /todos` with JSON `{"title": string, "done"?: boolean}` creates a todo and returns 201 with `{"id": string, "title": string, "done": boolean}`. `done` defaults to false. A missing or empty title returns 400 with `{"error": string}`.
- `GET /todos` returns 200 with an array of all todos. `GET /todos?done=true` or `?done=false` filters.
- `GET /todos/:id` returns 200 with the todo, or 404 with `{"error": string}`.
- `PATCH /todos/:id` with any of `title`, `done` updates and returns 200 with the todo; 404 if missing; 400 if `title` is present and empty.
- `DELETE /todos/:id` returns 204, or 404 if missing.
- Write tests for these routes with `bun test`.
