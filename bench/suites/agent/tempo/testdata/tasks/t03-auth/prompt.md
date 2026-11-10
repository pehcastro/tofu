Protect every route under `/api/*` with a bearer token middleware.

- The valid token is read from the environment variable `API_TOKEN` at request time.
- A missing `Authorization` header returns 401 with `{"error":"missing token"}`. A wrong token returns 403 with `{"error":"invalid token"}`.
- Add `GET /api/me` returning 200 with `{"ok":true}` for a valid token.
- Routes outside `/api` stay public.
- Write tests with `bun test`.
