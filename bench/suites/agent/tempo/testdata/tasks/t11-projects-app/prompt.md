Build a complete multi-tenant project management API, stored in SQLite with `bun:sqlite`, organized in modules under `src/`.

Data: organizations, users (belong to one organization, role `owner`, `admin` or `member`), projects (belong to an organization), tasks (belong to a project; `title`, `description`, `status` in `todo|doing|done`, `priority` 1 to 4, `assigneeId?`, `dueDate?` ISO date, `createdAt`, `updatedAt`), and comments on tasks.

- The database path comes from env `PM_DB`, default `pm.db`. Export `openDb(path)` from `src/db.ts`; run versioned migrations once.
- Auth: `POST /auth/register` `{orgName, email, password, name}` creates an organization and its owner. `POST /auth/login` returns an HS256 JWT (`JWT_SECRET` env, read at request time) with `sub`, `org` and `role`. Every other route needs `Authorization: Bearer`; 401 otherwise.
- Users: `POST /users` (owner or admin only, else 403) invites a user into the caller's organization; `GET /users`; `PATCH /users/:id/role` (owner only).
- Projects: CRUD under `/projects`; members can read, admins and owners can write.
- Tasks: CRUD under `/projects/:projectId/tasks`. List supports filters `status`, `assigneeId`, `priority`, `q` (title or description, case-insensitive), `dueBefore`, sorting `sort=dueDate|priority|createdAt` with `order=asc|desc`, and cursor pagination (`limit`, default 20, max 100; response `{"items", "nextCursor"}`). The assignee must be in the same organization (400 otherwise).
- `POST /tasks/:id/comments`, `GET /tasks/:id/comments`.
- `GET /stats` returns per-project counts of tasks by status for the caller's organization.
- Tenancy: no route ever returns or changes data of another organization; such ids return 404.
- Validation with zod; validation errors return 400 `{"error":"validation","issues":[...]}`; unknown ids 404 `{"error":"not found"}`.
- Write tests with `bun test` against a temporary database, covering auth, roles, tenancy, filters, sorting and pagination.
