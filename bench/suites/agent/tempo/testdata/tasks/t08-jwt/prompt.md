Add user accounts with JWT authentication, in-memory storage.

- `POST /auth/signup` `{email, password}`: password at least 8 characters, email must look valid, else 400 `{"error": string}`. Duplicate email returns 409. Store the password hashed with `Bun.password`. Returns 201 `{"id", "email"}`, never the password or hash.
- `POST /auth/login` `{email, password}` returns 200 `{"accessToken", "refreshToken"}`; wrong credentials return 401.
- Access tokens are HS256 JWTs signed with env `JWT_SECRET` (read at request time), valid 15 minutes, with `sub` = user id. Refresh tokens are opaque random strings valid 7 days.
- `POST /auth/refresh` `{refreshToken}` returns a new pair and invalidates the old refresh token (rotation); reuse of an old one returns 401.
- `POST /auth/logout` `{refreshToken}` invalidates it, 204.
- `GET /me` with `Authorization: Bearer <accessToken>` returns 200 `{"id", "email"}`; missing, bad or expired tokens return 401.
- Write tests with `bun test`.
