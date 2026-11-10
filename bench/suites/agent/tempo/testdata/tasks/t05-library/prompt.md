Build a complete library REST API, in-memory, organized in modules under `src/` (routes, validation, storage).

Resources:
- Authors: `{id, name, born?: number}`.
- Books: `{id, title, authorId, year, tags: string[], available: boolean}`.
- Loans: `{id, bookId, member: string, loanedAt: ISO string, returnedAt?: ISO string}`.

Routes:
- `/authors`: POST (201), GET list, GET `/:id`, PATCH, DELETE (409 with `{"error"}` if the author has books).
- `/books`: POST (201, 400 if `authorId` does not exist), GET list with query filters `authorId`, `tag`, `available`, `q` (case-insensitive title search), and pagination `page` and `limit` (default limit 10, max 50). The list response is `{"items": [...], "page": n, "limit": n, "total": n}`. GET `/:id`, PATCH, DELETE.
- `/loans`: POST `{bookId, member}` returns 201 and marks the book unavailable; 409 if the book is not available. POST `/loans/:id/return` sets `returnedAt` and makes the book available; 409 if already returned. GET `/loans?member=x&open=true`.
- `GET /authors/:id/books` lists that author's books.

Validation: use zod (`bun add zod`). Every validation error returns 400 with `{"error": "validation", "issues": [...]}`. Unknown ids return 404 with `{"error": "not found"}`. Unknown routes return 404 JSON.

Write tests with `bun test` covering every route and error.
